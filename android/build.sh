#!/usr/bin/env bash
# Builds FocusLock for Android into build/focuslock.apk without Gradle:
# aapt2 + javac + d8 + apksigner from the Android SDK.
set -euo pipefail
cd "$(dirname "$0")"
SDK=${ANDROID_HOME:-$HOME/Android/Sdk}
BT=$(ls -d "$SDK"/build-tools/* | sort -V | tail -1)
JAR=$(ls -d "$SDK"/platforms/android-* | sort -V | tail -1)/android.jar
KEY=${FOCUSLOCK_KEYSTORE:-$HOME/.config/FocusLock/android.keystore}

rm -rf build && mkdir -p build/classes build/gen build/assets build/dex
cp ../lockdown/blocklist.txt build/assets/

"$BT/aapt2" compile --dir res -o build/res.zip
"$BT/aapt2" link -I "$JAR" --manifest AndroidManifest.xml -A build/assets \
    --min-sdk-version 29 --target-sdk-version 36 --java build/gen \
    -o build/unsigned.apk build/res.zip
javac -source 11 -target 11 -Xlint:-options -classpath "$JAR" -d build/classes \
    $(find src build/gen -name '*.java')
"$BT/d8" --release --min-api 29 --lib "$JAR" --output build/dex $(find build/classes -name '*.class')
(cd build/dex && zip -q ../unsigned.apk classes.dex)
"$BT/zipalign" -f 4 build/unsigned.apk build/aligned.apk

# One key for every build, so updates install over the old app.
if [[ ! -f $KEY ]]; then
  mkdir -p "$(dirname "$KEY")"
  keytool -genkeypair -keystore "$KEY" -storepass focuslock -keypass focuslock -alias focuslock \
      -keyalg RSA -keysize 2048 -validity 10000 -dname "CN=FocusLock" >/dev/null
fi
"$BT/apksigner" sign --ks "$KEY" --ks-pass pass:focuslock --out build/focuslock.apk build/aligned.apk
echo "Built $(pwd)/build/focuslock.apk"
