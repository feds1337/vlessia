#!/usr/bin/env bash
# Builds the Go core (../mobile) into app/libs/vlessia.aar with gomobile.
# Needs Go, gomobile/gobind (go install golang.org/x/mobile/cmd/...) and the
# Android NDK (ANDROID_NDK_HOME or ANDROID_HOME/ndk/<version>).
set -euo pipefail
cd "$(dirname "$0")/.."

targets="${TARGETS:-android/arm64,android/arm,android/amd64}"
mkdir -p android/app/libs
gomobile bind \
  -target="$targets" \
  -androidapi 24 \
  -javapkg io.github.feds1337.vlessia \
  -trimpath \
  -ldflags="-s -w -buildid= -checklinkname=0" \
  -o android/app/libs/vlessia.aar \
  ./mobile
rm -f android/app/libs/vlessia-sources.jar
ls -l android/app/libs
