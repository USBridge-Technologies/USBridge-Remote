# IzzyOnDroid RFP draft

Submit at: **https://codeberg.org/IzzyOnDroid/repodata/issues/new** (issue
tracker moved off GitLab to Codeberg). Pick the "App request" template if one
is offered, otherwise a plain issue with this content:

---

**App name:** USBridge Client

**Package name:** `io.usbridge.client`

**Source code:** https://github.com/USBridge-Technologies/USBridge-Remote

**License:** GPL-3.0-only (see `client/LICENSE`; `client/moonlight-common-c`
submodule is GPL-3.0 too)

**Category:** Connectivity / System (remote KVM + remote-desktop client)

**Summary:** Hybrid remote-access client — BIOS-level KVM control via
USBridge-KVM hardware, plus lightweight software remote desktop
(Moonlight/Sunshine-protocol compatible, also interoperable with NVIDIA
GameStream-compatible hosts).

**Self-built, signed release (no self-update, F-Droid-style):**
This is a dedicated "market" build flavor with the in-app self-update path
(`internal/update`) compiled out entirely via a Go build tag — it never
fetches or installs executable code on its own, and doesn't request
`REQUEST_INSTALL_PACKAGES`. It's built and signed by our own GitHub Actions
CI, same key every release.

- Latest APK (stable URL, always the newest release):
  `https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/USBridgeClient-Android-arm64-market.apk`
- Releases page (for update-detection regex / changelog):
  `https://github.com/USBridge-Technologies/USBridge-Remote/releases`
- Release tags follow `release-YYYY.MM.DD` (combined multi-platform
  releases) — the Android asset name is always
  `USBridgeClient-Android-arm64-market.apk` regardless of tag, version is
  read from the APK's own manifest (`versionName`/`versionCode`, derived
  from `client/VERSION`).

**Note on architecture:** arm64-v8a only (no armeabi-v7a/x86 build produced
today) — flag if that's a blocker on your end, we can look into adding
another ABI if it's actually needed for inclusion.

---

Before opening this, worth a final read of IzzyOnDroid's own Inclusion
Policy wiki page for anything not covered above.
