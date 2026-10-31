#!/usr/bin/env bash
# 不使用 Gradle，直接用 Android SDK 命令行工具构建并签名 APK。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SDK="${ANDROID_SDK_ROOT:-/opt/android-sdk}"
BT="$SDK/build-tools/34.0.0"
PLATFORM="$SDK/platforms/android-34/android.jar"
JAVA_BIN="${JAVA_HOME:-/opt/jdk21}/bin"
BUILD="$ROOT/build"
DIST="$(cd "$ROOT/.." && pwd)/dist"
OUT="$DIST/wxmp-android.apk"
KS="$ROOT/wxmp-release.keystore"
KS_PASS="wxmp123456"
ALIAS="wxmp"
export PATH="$JAVA_BIN:$PATH"

for f in "$BT/aapt2" "$BT/d8" "$BT/zipalign" "$BT/apksigner" "$JAVA_BIN/javac" "$JAVA_BIN/keytool"; do
  if [[ ! -x "$f" ]]; then echo "缺少工具: $f" >&2; exit 1; fi
done
if [[ ! -f "$PLATFORM" ]]; then echo "缺少 android.jar: $PLATFORM" >&2; exit 1; fi

mkdir -p "$DIST"
rm -rf "$BUILD"
mkdir -p "$BUILD/gen" "$BUILD/obj" "$BUILD/dex"

echo "==> aapt2 compile"
"$BT/aapt2" compile --dir "$ROOT/res" -o "$BUILD/res.zip"

echo "==> aapt2 link"
"$BT/aapt2" link -o "$BUILD/base.apk" -I "$PLATFORM" \
  --manifest "$ROOT/AndroidManifest.xml" -R "$BUILD/res.zip" \
  --java "$BUILD/gen" --min-sdk-version 26 --target-sdk-version 34 \
  --version-code 1 --version-name 1.0.0 --auto-add-overlay

echo "==> javac"
find "$ROOT/src" "$BUILD/gen" -name '*.java' > "$BUILD/sources.txt"
"$JAVA_BIN/javac" --release 8 -encoding UTF-8 -classpath "$PLATFORM" \
  -d "$BUILD/obj" @"$BUILD/sources.txt"

echo "==> d8"
find "$BUILD/obj" -name '*.class' > "$BUILD/classes.txt"
"$BT/d8" --release --min-api 26 --lib "$PLATFORM" \
  --output "$BUILD/dex" @"$BUILD/classes.txt"

echo "==> package dex"
cp "$BUILD/base.apk" "$BUILD/unsigned.apk"
(cd "$BUILD/dex" && zip -q -j "$BUILD/unsigned.apk" classes.dex)

echo "==> zipalign"
"$BT/zipalign" -f -p 4 "$BUILD/unsigned.apk" "$BUILD/aligned.apk"

echo "==> keystore"
if [[ ! -f "$KS" ]]; then
  "$JAVA_BIN/keytool" -genkeypair -v -keystore "$KS" -storetype PKCS12 \
    -alias "$ALIAS" -keyalg RSA -keysize 2048 -validity 10000 \
    -storepass "$KS_PASS" -keypass "$KS_PASS" \
    -dname "CN=wxmp, OU=wxmp, O=wxmp, L=Beijing, S=Beijing, C=CN" >/dev/null 2>&1
fi

echo "==> sign"
"$BT/apksigner" sign --ks "$KS" --ks-key-alias "$ALIAS" \
  --ks-pass "pass:$KS_PASS" --key-pass "pass:$KS_PASS" \
  --out "$OUT" "$BUILD/aligned.apk"
"$BT/apksigner" verify --print-certs "$OUT" | head -6

echo "APK: $OUT ($(du -h "$OUT" | cut -f1))"
