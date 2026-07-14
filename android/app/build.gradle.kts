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
// with macOS/Linux/Windows/iOS builds); versionCode is the git commit count, a
// simple monotonically increasing counter Google Play requires per upload.
val appVersionName = rootProject.file("../VERSION")
    .takeIf { it.exists() }
    ?.readText()
    ?.trim()
    ?.takeIf { it.isNotEmpty() }
    ?: "1.0.0"

val appVersionCode = try {
    val process = ProcessBuilder("git", "rev-list", "--count", "HEAD")
        .directory(rootProject.projectDir)
        .redirectErrorStream(true)
        .start()
    val output = process.inputStream.bufferedReader().readText().trim()
    if (process.waitFor() == 0) output.toIntOrNull() ?: 1 else 1
} catch (e: Exception) {
    1
}

android {
    namespace = "com.usbridge.client"
    compileSdk = 34
    if (!detectedNdkVersion.isNullOrBlank()) {
        ndkVersion = detectedNdkVersion
    }

    defaultConfig {
        applicationId = "com.usbridge.client"
        minSdk = 26
        targetSdk = 34
        versionCode = appVersionCode
        versionName = appVersionName
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
