# Keep nbdbridge
-keep class nbdbridge.** { *; }

# Keep GoNativeActivity
-keep class org.golang.app.** { *; }

# Keep every JNI bridge class Fyne/Go native code calls by name+signature via
# reflection (GetMethodID). There is no static Kotlin/Java call site for these
# methods, so R8 sees them as unused and strips or renames them in release
# builds — the JNI lookup then fails at runtime. Confirmed by device testing
# (tests/test_android_ui_stress.sh): MainActivity.showFileOpen was missing in
# a release APK, logged as "cannot find method showFileOpen (Ljava/lang/String;)V"
# at Fyne init, then crashed the whole process later with a JNI
# "mid == null" abort the first time that cached (null) method ID was invoked.
-keep class com.usbridge.client.MainActivity { *; }
-keep class com.usbridge.client.GyroBridge { *; }
-keep class com.usbridge.client.NetworkBridge { *; }
-keep class com.usbridge.client.QRResultBridge { *; }
-keep class com.usbridge.client.KeyboardBridge { *; }
-keep class com.usbridge.client.GestureBridge { *; }
-keep class com.usbridge.client.VideoSurfaceBridge { *; }
-keep class com.usbridge.client.CameraHelper { *; }
-keep class com.usbridge.client.VulkanOverlayBridge { *; }
-keep class com.usbridge.client.HapticBridge { *; }
-keep class com.usbridge.client.NbdBridge { *; }
