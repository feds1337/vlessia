plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// The release workflow passes -PvlessiaVersion=v0.3.0; local builds are
// "dev" and never offer updates.
val appVersion = (findProperty("vlessiaVersion") as String?)?.removePrefix("v") ?: "dev"
val appVersionCode = Regex("""^(\d+)\.(\d+)\.(\d+)""").find(appVersion)?.destructured
    ?.let { (major, minor, patch) -> major.toInt() * 10000 + minor.toInt() * 100 + patch.toInt() } ?: 1

// Release signing comes from the environment (CI secrets); without it the
// release build is signed with the debug key.
val keystorePath: String? = System.getenv("VLESSIA_KEYSTORE")

android {
    namespace = "io.github.feds1337.vlessia"
    compileSdk = 35

    defaultConfig {
        applicationId = "io.github.feds1337.vlessia"
        minSdk = 24
        targetSdk = 35
        versionCode = appVersionCode
        versionName = appVersion
    }

    signingConfigs {
        if (keystorePath != null) {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = System.getenv("VLESSIA_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("VLESSIA_KEY_ALIAS")
                keyPassword = System.getenv("VLESSIA_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }

    // One APK per processor type keeps downloads small; the universal one
    // is for when in doubt.
    splits {
        abi {
            isEnable = true
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = true
        }
    }

    packaging {
        // Compressed native libraries: the Go library is most of the app.
        jniLibs.useLegacyPackaging = true
    }

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    sourceSets["main"].assets.srcDir(layout.buildDirectory.dir("generated/webAssets"))
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

// The UI is the desktop client's, from frontend/dist.
val copyWebAssets by tasks.registering(Copy::class) {
    from(rootProject.file("../frontend/dist"))
    into(layout.buildDirectory.dir("generated/webAssets/web"))
}
tasks.named("preBuild") { dependsOn(copyWebAssets) }

dependencies {
    // Built by build-core.sh with gomobile from ../mobile.
    implementation(files("libs/vlessia.aar"))
    implementation("androidx.core:core:1.16.0")
    implementation("androidx.webkit:webkit:1.14.0")
    // QR scanner with its own camera screen; needs no Google Play services.
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
}
