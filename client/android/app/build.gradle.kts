import java.util.Properties
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("com.google.gms.google-services")
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
// that's supposed to be new actually is. major/minor get two decimal
// digits (0-99 -- this project has sat on 3.0.x for its entire history, so
// that's enormous headroom for a component that essentially never moves)
// and patch, which moves roughly once per release and is the one that
// actually runs out, gets five (0-99999). Giving all three the same
// 0-9999 budget (requested, reasonably, for symmetry) isn't possible
// within Android's own versionCode ceiling (Play Console caps it at
// 2,100,000,000): 10000^3 alone overflows that by three orders of
// magnitude, so patch -- the one under real pressure -- gets the extra
// digit major/minor give up. The old 2-digit-everywhere version of this
// scheme silently *coerced* 3.0.104 down to 3.0.99 (coerceIn, not a build
// failure) and collided with that exact version's already-uploaded Play
// Store versionCode (30099) -- confirmed live, "Version code 30099 has
// already been used" on the 3.0.104 upload. require() below replaces that
// silent clamp with a loud build failure instead, so the next overflow
// (whenever major/minor/patch actually exceeds its new, much larger
// budget) fails obviously here rather than quietly reproducing the same
// collision.
val appVersionCode = appVersionName
    .split(".")
    .mapNotNull { it.toIntOrNull() }
    .let { parts ->
        if (parts.size != 3) null
        else {
            val (major, minor, patch) = parts
            require(major in 0..99) { "VERSION major '$major' out of range (0-99) for versionCode: $appVersionName" }
            require(minor in 0..99) { "VERSION minor '$minor' out of range (0-99) for versionCode: $appVersionName" }
            require(patch in 0..99999) { "VERSION patch '$patch' out of range (0-99999) for versionCode: $appVersionName" }
            major * 10_000_000 + minor * 100_000 + patch
        }
    }
    ?: 1

android {
    namespace = "io.usbridge.client"
    // Play Console requires new/updated apps to target API 36 (Android 16)
    // from 2026-08-31 (prior threshold was API 35) — bump both together
    // since targetSdk can't exceed compileSdk. Needs AGP >= 8.13 (see
    // build.gradle.kts) and SDK platform 36 installed; F-Droid's recipe
    // (metadata/io.usbridge.client.yml in fdroiddata) fetches this via
    // sdkmanager itself.
    compileSdk = 36
    if (!detectedNdkVersion.isNullOrBlank()) {
        ndkVersion = detectedNdkVersion
    }

    defaultConfig {
        applicationId = "io.usbridge.client"
        minSdk = 26
        targetSdk = 36
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

    buildFeatures {
        viewBinding = true
    }

    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

dependencies {
    implementation(platform("com.google.firebase:firebase-bom:35.0.0"))
    implementation("com.google.firebase:firebase-analytics")

    implementation("androidx.core:core-ktx:1.12.0")
    implementation("androidx.appcompat:appcompat:1.6.1")
    implementation("androidx.documentfile:documentfile:1.0.1")
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    implementation("com.google.android.play:review:2.0.2")
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))
}
