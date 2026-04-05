# kotlinx.serialization
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.AnnotationsKt
-keepclassmembers class kotlinx.serialization.json.** { *** Companion; }
-keepclasseswithmembers class kotlinx.serialization.json.** { kotlinx.serialization.KSerializer serializer(...); }
-keep,includedescriptorclasses class com.mediautopia.app.**$$serializer { *; }
-keepclassmembers class com.mediautopia.app.** { *** Companion; }
-keepclasseswithmembers class com.mediautopia.app.** { kotlinx.serialization.KSerializer serializer(...); }

# HiveMQ MQTT Client
-keep class com.hivemq.client.** { *; }
