# KMS screen capture on Linux (CAP_SYS_ADMIN)

KMS capture reads frames straight from DRM/KMS: no compositor, no portal
dialog, and it works before anyone logs in. Plain desktop capture works
without extra rights on current kernels. Capturing a fullscreen game's
direct-scanout buffer does not: that needs `CAP_SYS_ADMIN`.

Both streamers (Sunshine and RustShine) get that capability from one
small, root-owned launcher, **`usbridge-streamer-launch`**. You install it
once with a single `pkexec` password prompt. After that:

- **RustShine updates never ask again.** The streamer can be re-downloaded
  any number of times and KMS capture keeps working, with no "Grant" click
  after each update. This matters most on a remote session, where nobody
  is at the machine to type the password.
- **Neither streamer binary, nor anything else in your home directory,
  carries the capability.**

## Why the grant used to disappear

A Linux file capability (`setcap`) belongs to one specific file (inode).
The old setup put `cap_sys_admin` directly on the downloaded
`~/.config/usbridge-agent/usbridge-streamer/usbridge-streamer`. Every
RustShine update writes a **new** file, so every update silently dropped
the capability. The next restart then ran without KMS rights, and the
remote user was left looking at a portal prompt they couldn't click.

The old setup also had a security problem. Sunshine went through
`sunshine-capexec`, which had `cap_sys_admin` and lived in the
user-writable state dir, and it **exec'd whatever path it was given**. Any
process running as your user, including malware, could use it to get
`CAP_SYS_ADMIN` (close to root) without a password. That launcher is gone,
and the agent deletes the old copy (`capexec-runtime/`) on startup.

## How it works

```
/usr/local/libexec/usbridge/            root:root 0755
├── usbridge-streamer-launch            root:root 0755, cap_sys_admin=ep
├── allowed-uids                        root:root 0644, uids allowed to use it
├── empty/                              root-owned empty dir (see env below)
├── sunshine/                           root-owned copy of the bundled Sunshine tree
└── sunshine.sha256                     which bundled Sunshine build that copy came from
```

### RustShine: runs only signed builds

`usbridge-streamer-launch --run <bundle> -- <args>`:

1. Checks the caller's uid against `allowed-uids`. The file and its
   directory must be root-owned and not writable by group or others. This
   stops another local account from using the launcher to KMS-capture the
   console user's screen.
2. Loads the **release bundle** the agent keeps next to the streamer
   (`~/.config/usbridge-agent/usbridge-streamer/release/`), which holds:
   - `usbridge-streamer.tar.gz`, the archive exactly as downloaded;
   - `manifest.json` + `manifest.json.sig`, rust-shine's CI-signed release
     manifest (see `rust-shine/docs/RELEASE_SIGNING.md`).
3. Verifies the Ed25519 signature against the release public key compiled
   into the launcher (`internal/streamerlaunch.ReleasePublicKeyB64`, the
   same key the entitlement backend checks). It then checks that the
   archive's SHA-256 matches the signed entry for this platform and
   extracts `usbridge-streamer` from that same in-memory copy.
4. Copies those exact bytes into a `memfd`, **seals** it (no further
   writes possible), raises `CAP_SYS_ADMIN` into the ambient set, and
   execs the memfd. Nothing can swap the file between the check and the
   exec, because the exec'd bytes are the verified bytes.

The streamer on disk is never trusted: the launcher runs the signed
archive's contents. Nothing in rust-shine had to change for this, since
the manifest signing already existed.

### Sunshine: runs only its root-owned copy

Sunshine isn't signed by us, so the launcher trusts a location instead of
a signature. `usbridge-streamer-launch --run-sunshine -- <args>` execs only
`/usr/local/libexec/usbridge/sunshine/usr/bin/sunshine`, never a path the
caller passes in. Before every launch it re-checks that the whole tree is
root-owned and not group/world-writable. That includes the bundled
`usr/lib` shared libraries, which run with the same capability through
Sunshine's `RPATH=$ORIGIN/../lib`.

The grant copies the agent's staged Sunshine tree into place. The pkexec
script copies it into a root-owned temp directory first. Only then does it
compare the copy against a SHA-256 manifest the agent computed beforehand,
checking both the hashes and the exact file list. So a file swapped or
added mid-copy is rejected, not installed.

When an agent update ships a newer Sunshine, the previously installed copy
**keeps running with KMS**, so the remote session survives. The Screen
Capture permission then shows as not granted, meaning "refresh available".
Clicking Grant locally installs the new copy.

### Environment

A process with an ambient capability is **not** in glibc's
secure-execution mode, so `LD_PRELOAD` and friends would still be honored.
The launcher therefore passes only an allowlist of variables through:
session locators (`WAYLAND_DISPLAY`, `XDG_RUNTIME_DIR`, `DBUS_…`,
`DISPLAY`, …), locale, `RUST_LOG`, `USBRIDGE_*`, and rust-shine's own
plain-value knobs. On top of that it:

- forces `PATH` to the system directories;
- pins `VK_DRIVER_FILES`/`VK_ICD_FILENAMES` to the root-owned system Vulkan
  ICD manifests and sets `VK_LOADER_LAYERS_DISABLE=~implicit~`, so no
  Vulkan driver or layer (both are shared libraries) can come from a JSON
  file in your home directory;
- points `XDG_DATA_HOME` at the root-owned empty dir. It does the same for
  `XDG_CONFIG_HOME` for RustShine. Sunshine keeps the real one because it
  stores its appdata there and won't start without it;
- sets `PULSE_COOKIE` so audio auth survives the moved config dir.

## Updates without losing the remote session

`App.stageRustShine` → `entitlement.StageRustShineVerified`:

1. Downloads the new build and checks its SHA-256 against the backend.
2. Extracts it into `usbridge-streamer.next/`. The running streamer isn't
   touched.
3. Saves the signed bundle next to it. The backend's `/v1/download/*` now
   returns `manifest_b64` + `manifest_sig`.
4. **If the launcher is installed**, asks the installed launcher itself
   (`--verify`) whether it would run this bundle with its capability
   effective. If it wouldn't, or if the download came without a verifiable
   manifest, the update is **refused** and the current build keeps running
   with KMS. The error is logged and the update is retried on the next
   check.
5. Moves the bundle into place, then the binary, and restarts the streamer
   (a few seconds; clients reconnect). `App.syncSunshineCapExec` re-checks
   the launcher against the new bundle before that restart.

A build staged before the backend passed the manifest through gets
re-staged once, same version, so it gains a bundle.

## The grant, step by step

The Screen Capture permission's **Grant** button (`permissions.RequestKMSCapture`)
runs one `pkexec /bin/sh -c …` that:

1. copies the bundled launcher into `/usr/local/libexec/usbridge/` as
   root, **then** checks its SHA-256 (so a user-writable source swapped
   mid-install never gets the capability), then runs
   `setcap cap_sys_admin=ep` on that root-owned copy;
2. adds your uid to `allowed-uids`;
3. for Sunshine, also installs the root-owned tree (see above).

Once the launcher is installed, the agent drops any legacy `setcap` on the
staged `usbridge-streamer` on startup by rewriting the file (a new inode
never inherits `security.capability`).

A new agent release re-prompts only if it needs a newer launcher protocol
(`streamerlaunch.MinProtocol`). Ordinary launcher changes don't re-prompt;
bump the protocol when a fix must reach installed launchers.

## Checking it

```sh
/usr/local/libexec/usbridge/usbridge-streamer-launch --version
getcap /usr/local/libexec/usbridge/usbridge-streamer-launch     # cap_sys_admin=ep
/usr/local/libexec/usbridge/usbridge-streamer-launch \
    --verify ~/.config/usbridge-agent/usbridge-streamer/release  # version=… cap=1
grep -E 'Cap(Eff|Amb)' /proc/$(pgrep -f '^usbridge-streamer ')/status
```

`usbridge-streamer` launched this way shows `comm` as its memfd fd number
(`ps -o comm`). Its `argv[0]` stays `usbridge-streamer`, and that is what
the agent's orphan cleanup matches on (`killall` alone wouldn't find it).

Launcher exit codes: `2` usage, `3` bundle/tree didn't verify, `4` no
effective `CAP_SYS_ADMIN` (not setcap'd, `nosuid` mount, or
`no_new_privs`), `5` uid not allowed, `6` exec failed. On any of these the
agent falls back to a plain launch (no KMS rights) instead of failing to
stream.

## Security tests

These run on every `go test ./...`, with no root and no network:

| Test | Guards against |
|---|---|
| `cmd/usbridge_streamer_launch` `TestLauncher_RefusesUnsignedAndTamperedBundles` | running a bundle signed by another key, or a real signed manifest paired with a different archive. A sentinel binary proves nothing was exec'd |
| `…TestLauncher_UserCopyNeverRuns` | a non-installed copy (no capability / uid not allowed) exec'ing anything, in either mode |
| `…TestLauncher_Usage` | the old `capexec <any-binary>` calling convention still working |
| `internal/streamerlaunch` `TestRealReleaseManifestVerifies` | the compiled-in key drifting from the one CI signs with (real v0.3.86 manifest in `testdata/`); any single flipped byte must fail |
| `…TestLoadVerified_Rejects`, `TestSanitizedEnv*`, `TestUIDAllowed_*`, `TestCheckRootOwned_*` | tampering, `LD_PRELOAD`/`VK_*`/`PATH`/XDG injection, user-writable allowlist or tree |
| `internal/entitlement` `TestStageRustShineVerified_*`, `TestStageRustShine_OldBackendWithoutManifest` | an update replacing a build the launcher would refuse; staging before verification |
| `internal/permissions` `TestStreamerLauncherInstallScript_HashBeforeSetcap`, `TestSunshineTreeInstallScript_ChecksBeforeReplacing` | `setcap` on an unchecked or user-writable file; replacing the Sunshine tree before hash + file-list checks |
| `internal/streamhost` `TestKillOrphansByArgv0_*` | orphaned memfd-launched streamers surviving cleanup, or cleanup killing the wrong process |

These run automatically whenever the launcher is installed on the test
machine (they never prompt):
`TestInstalled_RefusesUnsignedAndTamperedBundles` and
`TestInstalled_TrustsOnlyRootOwnedFiles`.

These need a real release bundle (see the header of
`cmd/usbridge_streamer_launch/launcher_test.go`):

```sh
export USBRIDGE_LIVE_BUNDLE=… USBRIDGE_LIVE_ENTITLEMENT=… USBRIDGE_LIVE_HWID=…
go test -run Live -v ./cmd/usbridge_streamer_launch/ ./internal/entitlement/
```

- `TestLive_StreamerGetsOnlyCapSysAdmin` checks that the real streamer ends
  up with `CapEff`/`CapPrm`/`CapAmb` = `cap_sys_admin` only, even with a
  hostile `LD_PRELOAD`.
- `TestLive_StreamerDiesWithParent` checks that the streamer dies with the
  agent (PDEATHSIG). This caught a real bug: without `LockOSThread` in the
  launcher the streamer outlived its parent.
- `TestLive_UpdateGatedByInstalledLauncher` stages the real signed release,
  then checks that a tampered one is refused.

`internal/permissions` `TestLiveGrant_*` perform the real pkexec install
(password prompt) and only run when asked. See that file's header.

## Removing it

```sh
sudo rm -rf /usr/local/libexec/usbridge
```

## Residual risks, stated plainly

- Any process running **as an allowed user** can start a genuine, signed
  `usbridge-streamer` (or the installed Sunshine) with `CAP_SYS_ADMIN` and
  arguments of its choosing. This is inherent to "KMS capture without a
  password prompt per launch". It's much narrower than before: the old
  setup handed out `CAP_SYS_ADMIN` for arbitrary code.
- Any **older** signed RustShine release can be run the same way. There is
  no downgrade floor yet.
- Libraries the streamers load from system paths, and their config files
  in `$HOME` (e.g. Mesa's `drirc`, PulseAudio's `client.conf`), are still
  read. Those are configuration, not code. Vulkan, the one loader that
  would take code from `$HOME`, is pinned as described above.

## Rotating the rust-shine release key

Besides the steps in `rust-shine/docs/RELEASE_SIGNING.md`, update
`ReleasePublicKeyB64` in `agent/internal/streamerlaunch/bundle.go`, and
bump `Protocol`/`MinProtocol` so installed launchers get replaced. An
installed launcher only trusts the key it was built with.
