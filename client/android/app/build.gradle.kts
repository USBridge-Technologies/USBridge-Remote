import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val localProperties = Properties().apply {
    val file = rootProject.file("local.properties")
    if (file.exists()) {
        file.inputStream().use { load(it) }
    }
}

val ndkDirFromEnv = providers.environmentVariable("ANDROID_NDK_HOME").orNull
    ?: providers.environmentVariable("ANDROID_NDK_ROOT").orNull

val detectedNdkVersion = ndkDirFromEnv
    ?.let { file(it.replace('\\', '/')) }
    ?.resolve("source.properties")
    ?.takeIf { it.exists() }
    ?.readLines()
    ?.firstOrNull { it.startsWith("Pkg.Revision") }
    ?.substringAfter('=')
    ?.trim()

// versionName mirrors the repo-wide VERSION file (single source of truth shared
// with macOS/Linux/Windows/iOS builds).
val appVersionName = rootProject.file("../VERSION")
    .takeIf { it.exists() }
    ?.readText()
    ?.trim()
    ?.takeIf { it.isNotEmpty() }
    ?: "1.0.0"

// versionCode used to be `git rev-list --count HEAD` -- a monotonically
// increasing counter in theory, but one that's completely decoupled from
// appVersionName above and fragile in exactly the ways that matter for a
// release artifact: it doesn't move at all when VERSION is bumped without
// also committing (a real build run against a dirty tree keeps the old
// count), a shallow CI checkout (`git clone --depth=1`, `actions/checkout`'s
// default) makes `rev-list --count HEAD` return 1 every single build
// instead of an increasing number, and a rebase/squash can renumber or even
// *decrease* it. Any of those silently reproduces "installed/shared build
// shows the previous version": Google Play (and Android's own package
// installer, outside of a plain `adb install -r`) refuses to treat an
// upload/install as an update unless its versionCode is strictly greater
// than what's already installed -- if it isn't, the old APK (and its old
// versionName, old everything) is exactly what a user keeps seeing, with no
// error surfaced anywhere.
//
// Deriving it from appVersionName itself instead removes that dependency
// entirely: it's deterministic, reproducible from a shallow/fresh checkout,
// and -- since it's parsed from the exact same file that already gates
// every other platform's build -- guaranteed to move whenever the version
// that's supposed to be new actually is. major.minor.patch each get two
// decimal digits (i.e. capped at 99), which comfortably covers this
// project's actual version history (2.1.x) with a lot of headroom left.
val appVersionCode = appVersionName
    .split(".")
    .mapNotNull { it.toIntOrNull()?.coerceIn(0, 99) }
    .let { parts ->
        if (parts.size == 3) parts[0] * 10000 + parts[1] * 100 + parts[2] else null
    }
    ?: 1

android {
    namespace = "io.usbridge.client"
    compileSdk = 34
    if (!detectedNdkVersion.isNullOrBlank()) {
        ndkVersion = detectedNdkVersion
    }

    defaultConfig {
        applicationId = "io.usbridge.client"
        minSdk = 26
        targetSdk = 34
        versionCode = appVersionCode
        versionName = appVersionName
    }

    // Two distribution channels, one codebase — see
    // src/main/AndroidManifest.xml's comment and
    // client/internal/update/update_disabled.go.
    //   - "market": Play Store / F-Droid / any channel that must not fetch
    //     or install executable code on its own. Default; no self-update.
    //   - "direct": the existing off-market GitHub Releases build, with the
    //     in-app self-update flow intact.
    // Neither flavor gets a Gradle signingConfig on purpose: both keep
    // producing plain unsigned outputs (app-<flavor>-release-unsigned.apk,
    // app-<flavor>-release.aab), and client/scripts/build_android_gradle.sh
    // signs whichever of those it needs externally (apksigner for the APK,
    // jarsigner for the AAB — apksigner itself is APK-only) with the same
    // release keystore either way. Attaching a signingConfig to a flavor
    // signs *both* its assemble and bundle outputs and drops the
    // "-unsigned" APK filename entirely, which the script doesn't expect —
    // learned that the hard way, keep it this way.
    flavorDimensions += "distribution"
    productFlavors {
        create("market") {
            dimension = "distribution"
        }
        create("direct") {
            dimension = "distribution"
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        viewBinding = true
    }

    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.12.0")
    implementation("androidx.appcompat:appcompat:1.6.1")
    implementation("androidx.documentfile:documentfile:1.0.1")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))
}
