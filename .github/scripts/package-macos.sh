#!/usr/bin/env bash
set -euo pipefail

app=build/bin/Vlessia.app
dmg="dist/Vlessia-$VERSION-macos-universal.dmg"
if [[ -n "$CERTIFICATE" ]]; then
  : "${SIGNING_IDENTITY:?Set MACOS_SIGNING_IDENTITY}"
  : "${APPLE_ID:?Set MACOS_NOTARY_APPLE_ID}"
  : "${APPLE_TEAM_ID:?Set MACOS_NOTARY_TEAM_ID}"
  : "${APPLE_PASSWORD:?Set MACOS_NOTARY_APP_PASSWORD}"
  keychain="$RUNNER_TEMP/vlessia-signing.keychain-db"
  p12="$RUNNER_TEMP/vlessia-signing.p12"
  keypass="$(openssl rand -hex 24)"
  trap 'security delete-keychain "$keychain" || true; rm -f "$p12"' EXIT
  printf '%s' "$CERTIFICATE" | base64 -D > "$p12"
  security create-keychain -p "$keypass" "$keychain"
  security unlock-keychain -p "$keypass" "$keychain"
  security import "$p12" -k "$keychain" -P "$CERTIFICATE_PASSWORD" -T /usr/bin/codesign
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$keypass" "$keychain"
  codesign --force --timestamp --options runtime --keychain "$keychain" --sign "$SIGNING_IDENTITY" "$app"
  codesign --verify --deep --strict "$app"
else
  echo 'macOS signing certificate not configured'
  codesign --force --sign - "$app"
fi

mkdir -p dist
size=$(( $(du -sm "$app" | cut -f1) + 50 ))
for attempt in 1 2 3; do
  hdiutil create -volname Vlessia -srcfolder "$app" -ov -format UDZO -size "${size}m" "$dmg" && break
  [[ "$attempt" != 3 ]] || exit 1
  sleep 10
done
if [[ -n "$CERTIFICATE" ]]; then
  codesign --force --timestamp --keychain "$keychain" --sign "$SIGNING_IDENTITY" "$dmg"
  xcrun notarytool submit "$dmg" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_PASSWORD" --wait
  xcrun stapler staple "$dmg"
  xcrun stapler validate "$dmg"
fi
