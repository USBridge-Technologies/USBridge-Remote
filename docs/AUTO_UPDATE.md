# Auto-update

Both the Client and the Agent check for a newer signed release every time
they launch. On every platform except iOS, if one is found, a confirm
dialog ("Update Available — version X.Y.Z is available, update now?") asks
before installing it — a forced silent update was jarring for a window the
user is actively looking at. This document is the design reference; the
code itself lives in `client/internal/update/` and `agent/internal/update/`
(near-identical packages, one per Go module).

The **check** itself is still unconditional and silent (a background
network call, never surfaced unless it finds something newer); only the
**apply** step waits for confirmation, and only where there's a GUI to ask
through:

- **Client**: always asks, on every platform it self-updates on
  (Windows/Linux/macOS/Android). The dialog is anchored to the main window
  and fires from `MainWindow.SetOnReadyCallback`, so the network round-trip
  never delays first paint.
- **Agent**: a `--headless` launch (no GUI, e.g. a systemd/launchd service)
  has no one to ask, so it keeps applying silently
  (`internal/update.CheckAndApply`) — see `internal/app.Start`. A normal
  GUI launch that owns its own engine asks the same way the client does,
  from `internal/ui.Window.ShowAndRun`. A GUI window that only *attached*
  to an already-running headless instance (`runThinClientGUI`) never
  offers the prompt at all: applying an update there would replace the
  on-disk binary and relaunch that attach-only process without ever
  touching the separate headless instance actually running the engine, so
  it's intentionally not wired up — restart the primary (headless) instance
  to pick up an update in that deployment shape.

## Why iOS is excluded

Apple's App Store is the only sanctioned way to update an iOS app; sideloaded
binary replacement isn't possible in a normal install anyway. `CheckAndApply`
short-circuits immediately on `GOOS == "ios"`.

## MITM resistance

The whole point of this feature is that it must stay safe even if the
download itself is intercepted — a MITM proxy, a poisoned DNS answer, a
compromised CDN edge in front of GitHub, a malicious update sitting on a
public Wi-Fi network, etc. That's done with two layers of verification:

1. **The manifest is Ed25519-signed.** CI (`scripts/sign_update_manifest.go`)
   builds `manifest-client.json` / `manifest-agent.json` — version number
   plus the exact SHA-256 of every platform's release artifact — and signs
   those exact bytes with a private key that exists **only** as a GitHub
   Actions secret (`CLIENT_UPDATE_ED25519_PRIVATE_KEY` /
   `AGENT_UPDATE_ED25519_PRIVATE_KEY`). The running app fetches the manifest
   and its detached signature from the latest GitHub Release and verifies
   the signature against the matching public key **compiled into the
   binary** (`pubkey.go`) before trusting a single field of it.
2. **The downloaded artifact is SHA-256-checked against that (now-trusted)
   manifest.** Only once step 1 passes does the recorded hash get used to
   validate what was actually downloaded.

An attacker controlling the network path can, at best, serve stale bytes
(which fail their own recorded hash) or something unrelated (same
failure). They cannot get this binary to accept anything that wasn't
produced by whoever holds the private key, because nothing is trusted
until the manifest signature checks out.

On macOS and Android, this is deliberately **on top of**, not instead of,
each platform's own signing story:

- **macOS**: the downloaded `.dmg`'s `.app` is independently re-verified
  with `codesign --verify --deep --strict` and `spctl --assess` — the same
  Developer ID / notarization checks `.github/workflows/*-release.yml`
  already runs as a release gate. A compromised update-signing key alone
  still isn't enough; the payload would also need to be signed with
  USBridge's real Apple Developer ID certificate.
- **Android**: this package verifies the manifest signature and the APK's
  SHA-256, then hands off to the OS's own installer UI (see below) — Android
  refuses to install an update APK unless it's signed with the exact same
  certificate as the currently-installed app (`ANDROID_KEYSTORE_BASE64` in
  CI), which is a hard OS-level guarantee this update path leans on rather
  than duplicates.

Windows and Linux have no equivalent OS-level trust chain available here
(no purchased Authenticode certificate; Linux has no standard code-signing
story at all), so the Ed25519 layer is the *only* integrity guarantee on
those two platforms. That's why they needed their own keypairs generated —
see `~/usbridge-update-keys/README.md` (intentionally kept outside this
repo; ask whoever generated it, or regenerate and re-embed if it's lost).

## Update channel

Both apps only ever fetch `https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download/manifest-{client,agent}.json`.
GitHub resolves `/releases/latest` to whichever release currently has
`make_latest` set — only `release-all.yml`'s combined release ever sets
that, and only when `prerelease` is false. Test builds
(`release-all-test.yml`, tags prefixed `test-`) are pre-releases and never
touch `/releases/latest`, so they're structurally invisible to the update
checker. `client-release.yml` and `agent-release.yml` (the narrower,
tag-triggered per-app pipelines) don't build a manifest at all, for the
same reason — their releases never become `/releases/latest` either, and
`client-release.yml` in particular only builds macOS + iOS, so it couldn't
produce a complete cross-platform manifest anyway.

## Per-platform apply behavior

| Platform | Mechanism |
|---|---|
| Windows | Extracts the downloaded zip to a staging dir, then hands off to a detached PowerShell helper (embedded via `go:embed`) that waits for this process to exit, copies the new files over the install dir, and relaunches — Windows won't let a running process overwrite its own loaded exe/DLLs. If the install dir needs admin rights, the helper re-launches itself elevated (`Start-Process -Verb RunAs`), which triggers one UAC prompt — an OS security control, not an extra confirmation step this feature adds. |
| Linux | AppImages are a single file: the new one is written next to the running one and swapped in with an atomic same-directory `os.Rename` — safe even though it's replacing the currently-executing binary, since Linux keeps the already-running process's inode alive until it exits. Then it re-execs itself and exits. |
| macOS | Mounts the `.dmg`, verifies Apple code signature (see above), `ditto`-copies the new `.app` next to the installed one, atomically swaps them, relaunches with `open -n`. |
| Android | Can't silently replace its own APK (no such permission, and doing so would forgo the OS's own signature check). After the user confirms, verifies manifest + APK hash, then opens the GitHub release page in the browser so the user completes the install through Android's normal `PackageInstaller` flow — a second, OS-owned confirmation on top of this package's own dialog, and one there's no unprivileged way to skip on stock Android. |
| iOS | Not applicable — see above. |

Every failure mode (offline, GitHub unreachable, bad signature, corrupted
download, no write permission to the install dir, ...) is logged and
swallowed; the app just starts up normally on its current version. A
mandatory update check must never be able to brick a working install.

## TODO(rustshine)

This only covers the open-source Sunshine-based agent build actually
published via GitHub Releases today. A closed-source "Rustshine"
streaming-host variant isn't published on GitHub yet and has no release
channel for this package to check against — revisit once that build gets
its own distribution/signing pipeline.
