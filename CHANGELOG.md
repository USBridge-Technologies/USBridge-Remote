# Changelog

All notable changes to USBridge (client + agent) are documented in this file.

## [2.0.20]

### Removed
- **FRP/QUIC tunnel support removed entirely.** The client and agent now connect only via
  direct LAN or Tailscale — `ConnectionProtocolQUIC`, `FRPService`, all `frp_*` config fields,
  the `quic_token` deep-link/QR parameter, and every FRP-specific code path in the connection
  manager, disk/NBD widgets, video widget, and master-sync flow have been deleted.
  Existing saved connections that used the old `quic` protocol value now fall back to `direct`.

### Fixed
- **Android release builds are now reproducibly signed.** CI previously signed every Android
  APK with a fresh, auto-generated debug keystore on each runner, so consecutive releases
  couldn't be installed as upgrades over one another (Android rejects installs signed by a
  different key). The `release-all` workflow now decodes a real release keystore from the
  `ANDROID_KEYSTORE_BASE64` secret and signs with it via `ANDROID_KEYSTORE_PASSWORD` /
  `ANDROID_KEY_ALIAS` / `ANDROID_KEY_PASSWORD`, and a new verification step fails the build if
  the resulting APK isn't actually signed with the release key.
- Fixed the Android CI job decoding secrets with `base64 -o`, which isn't supported by
  ubuntu-latest's newer coreutils; switched to a plain output redirect.
- Fixed the Android Gradle build referencing the old `nbdbridge` Go package/directory after it
  was renamed to `androidbridge`, which broke `gomobile bind` in CI ("no exported names in the
  package ./nbdbridge").

### Changed
- Bumped client and agent version to 2.0.20.

## Earlier

See git history prior to this file's introduction for changes before 2.0.20.
