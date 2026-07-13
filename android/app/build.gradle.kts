plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

// Single source of truth for the app version — the top-level android/VERSION
// file (Major.Minor.Patch), bumped per the project's versioning convention.
val appVersionName = rootProject.projectDir.resolve("VERSION").readText().trim()

android {
    namespace = "com.walkietalkie"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.walkietalkie"
        // 29 (not 26, unlike the ClonesApp precedent): MediaCodec's Opus
        // *encoder* (audio/opus, CONFIGURE_FLAG_ENCODE) was only added in
        // Android 10 — the decoder is older, but encode is what mic capture
        // needs. See audio/OpusMicSource.kt.
        minSdk = 29
        targetSdk = 35
        versionCode = 1
        versionName = appVersionName
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
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
        compose = true
        buildConfig = true
    }
}

dependencies {
    // core/mobile + core/media bound to Android via gomobile — see
    // tools/gomobile-bind-android.sh. Rebuild that before building this app
    // whenever core/ changes.
    implementation(files("libs/core.aar"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.service)
    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.ui)
    implementation(libs.androidx.ui.graphics)
    implementation(libs.androidx.ui.tooling.preview)
    implementation(libs.androidx.material3)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.play.services.location)
    debugImplementation(libs.androidx.ui.tooling)
    debugImplementation(libs.androidx.ui.test.manifest)
}
