# F-Droid / IzzyOnDroid distribution — process notes

This doc exists so the whole submission/update process doesn't have to be
re-derived from scratch next time. No secrets in here — everything below is
public (GitLab MR, Codeberg issue, recipe content).

## What's shipped where

- **Google Play**: `.aab`, built by CI (`market` Gradle flavor), published via
  `release-all.yml`'s `client-android-market` job.
- **GitHub Releases (direct download)**: `USBridgeClient-Android-arm64-selfupdate.apk`
  — `direct` flavor, has the in-app self-updater compiled in.
- **F-Droid / IzzyOnDroid**: `USBridgeClient-Android-arm64-market.apk` — same
  `market` flavor as Play, no self-update, no `REQUEST_INSTALL_PACKAGES`. This
  is the file both F-Droid's own buildserver (from source) and IzzyOnDroid
  (repackaging our signed release) end up shipping.

## Official F-Droid

- Recipe: `docs/fdroid/io.usbridge.client.yml` — mirrors
  `metadata/io.usbridge.client.yml` in the fdroiddata repo. Keep both in sync;
  this copy is just for having it under version control here too.
- Submitted as MR: **https://gitlab.com/fdroid/fdroiddata/-/merge_requests/45184**
  from fork **https://gitlab.com/itsme228/fdroiddata** (branch
  `add-io-usbridge-client`).
- **No auto-update.** `AutoUpdateMode: None` / `UpdateCheckMode: None` — F-Droid
  does not watch the GitHub repo. Every new release needs a manual recipe
  update + MR (see below). This was a deliberate choice: our `versionCode` is
  derived from `client/VERSION`, not from the git tag itself (tags are
  `release-YYYY.MM.DD` and bundle all platforms), so naive tag-tracking
  wouldn't compute the right versionCode without extra `UpdateCheckData`
  regex config — and even with that, F-Droid's buildserver still wouldn't
  *rebuild* unattended given how many custom steps this recipe has (Go, NDK,
  gomobile, moonlight-common-c/openssl/opus from source). Manual MR per
  release is the realistic path for an app this custom.

### To ship a new version to F-Droid

1. In `docs/fdroid/io.usbridge.client.yml`, add a new entry at the top of
   `Builds:` with the new `versionName` / `versionCode` / `commit` (must be a
   commit reachable from `main`).
2. Bump `CurrentVersion` / `CurrentVersionCode` to match.
3. Re-validate locally (see "Local validation" below) before pushing anywhere.
4. Push to the fork branch (`itsme228/fdroiddata`, `add-io-usbridge-client`)
   via the GitLab API (see "Pushing to the fork" below), which updates the
   already-open MR #45184 automatically (no need to open a new one, unless
   it's since been merged — then open a fresh MR/branch).

### Local validation (do this before pushing to the fork)

The whole point of this workflow: fdroidserver's actual output depends on
exact tool versions, which pip's defaults do **not** match. Always test
against a venv pinned to what their GitLab CI actually installs (check a
recent pipeline's apt install log for exact versions if these drift):

```bash
python3 -m venv /tmp/fdroid242venv
/tmp/fdroid242venv/bin/pip install fdroidserver==2.4.2
/tmp/fdroid242venv/bin/pip install "ruamel.yaml==0.18.10"   # NOT PyYAML -- see gotchas below
```

Clone/refresh a local fdroiddata checkout, drop the recipe into
`metadata/io.usbridge.client.yml`, then:

```bash
FDROID=/tmp/fdroid242venv/bin/fdroid
$FDROID rewritemeta io.usbridge.client   # canonicalize
$FDROID rewritemeta io.usbridge.client   # run again -- must produce ZERO diff (idempotency)
pipx run check-jsonschema --schemafile schemas/metadata.json metadata/io.usbridge.client.yml
$FDROID lint io.usbridge.client
```

Only copy the result back into this repo (and push to the fork) once
rewritemeta is idempotent, schema passes, and lint is clean. Pushing anything
that still gets reformatted by their CI wastes a ~2-9 minute pipeline cycle
per fix.

### Pushing to the fork (no full local clone needed)

```bash
B64=$(base64 < docs/fdroid/io.usbridge.client.yml | tr -d '\n')
python3 -c "
import json
print(json.dumps({
  'branch': 'add-io-usbridge-client',
  'encoding': 'base64',
  'content': '$B64',
  'commit_message': '...'
}))" > /tmp/payload.json
glab api --method PUT projects/itsme228%2Ffdroiddata/repository/files/metadata%2Fio.usbridge.client.yml \
  --input /tmp/payload.json
```

Then watch the pipeline it triggers:

```bash
glab api "projects/itsme228%2Ffdroiddata/pipelines?ref=add-io-usbridge-client" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)[0]['id'])"
glab api "projects/itsme228%2Ffdroiddata/pipelines/<id>/jobs"
glab api "projects/itsme228%2Ffdroiddata/jobs/<job_id>/trace"   # full log if a job fails
```

### Gotchas found the hard way (avoid re-discovering these)

- **fdroidserver dumps YAML with `ruamel.yaml`, not PyYAML.** Their .deb pulls
  a much newer `ruamel.yaml` than pip's default resolution for the same
  fdroidserver version (pip's own declared constraint is stale vs. what
  Debian actually ships). Pin to the real version or `rewritemeta` output
  won't match and their `fdroid rewritemeta` CI job will "fail" purely on
  formatting.
- **Comments inside `Builds:` (including inside script fields) get silently
  dropped on every rewritemeta round-trip** — even a top-of-file header
  comment gets wiped. Put anything worth keeping in `MaintainerNotes:`
  instead (a real schema field, survives round-tripping).
- **A single unbreakable long token (e.g. a URL) forced to wrap** can leave a
  trailing space before the line break, which `fdroid lint` then flags.
  Sidestep by assigning the long token to a shell variable on its own line
  (nothing to wrap: `VAR="https://...`" has no spaces in it at all) and
  referencing `$VAR` on a separate short line.
- **Each build phase (`sudo:`, `init:`, `prebuild:`/`build:`) is a fresh
  shell.** Exports from one don't carry to the next — re-export `PATH`,
  `GOOS`, `CC`, etc. in every phase that needs them.
- **PATH bootstrap chicken-and-egg**: `export PATH="x:$PATH:$(go env
  GOPATH)/bin"` fails if `go` isn't on `$PATH` yet *within that same
  statement* (command substitution evaluates before the assignment takes
  effect). Split into two sequential `export PATH=...` lines.
- **The source scanner runs after `prebuild:` but before `build:`/`gradle:`**
  (confirmed by reading `fdroidserver/build.py`: "Scan before building...").
  Anything that generates `.so`/`.a`/`.aar`/`.jar` files must live in
  `build:`, not `prebuild:`, or the scanner flags your own build output as
  smuggled binaries.
- **Pre-existing checked-in binaries** (e.g. `androidbridge-sources.jar`,
  `mobile-sources.jar` — small companion jars already tracked in this repo)
  always get flagged regardless of build/prebuild placement. List them under
  `scandelete:`.
- **The buildserver has no `sudo` binary** inside the build script's own
  shell (privilege escalation, if any, happens one layer up in
  fdroidserver's own harness) — don't `sudo` inside `init:`/`build:`. Install
  the Go toolchain into `$HOME`, not `/usr/local` (not writable anyway).
- **The buildserver's SDK image only has the deprecated `tools/` package**,
  whose `sdkmanager` crashes under a modern JDK (`NoClassDefFoundError:
  javax/xml/bind/...` — JAXB was removed from the JDK in 9+). Fetch a current
  `commandlinetools-linux-*.zip` from `dl.google.com` directly and point its
  `sdkmanager` at the existing SDK root via `--sdk_root=/opt/android-sdk`.
- **Missing SDK platforms**: `gomobile bind -androidapi 26` needs
  `platforms;android-26` actually installed, not just NDK. Install the
  platforms/build-tools you need explicitly via `sdkmanager` before use.
- **`gomobile bind -target android` (no arch suffix) builds all 4 ABIs.**
  Always scope to `-target android/arm64`.
- **Multi-module Gradle projects** (root project + nested `app/` module, our
  case: `subdir: client/android` containing `app/`) confuse fdroidserver's
  automatic APK-output discovery, which only looks directly under
  `<subdir>/build/outputs/apk`. Set `output:` explicitly to the real glob
  path (e.g. `app/build/outputs/apk/market/release/*.apk`) to bypass
  autodetection entirely.
- **GitLab CI job logs cap at 4MB** — a verbose sub-build (CMake configure
  noise, repeated per sub-project) can blow through that before the actual
  error ever prints, making a real failure look like a silent timeout.
  Redirect noisy commands to a log file and only `tail` it on failure.
- **`go install <tool>@version` cross-compiles for whatever `GOOS`/`GOARCH`/
  `CC` happen to be exported at the time** — installing a *host* tool (like
  `fyne`) while Android cross-compile env vars are still active silently
  produces an Android binary at the wrong path. Use `env -u CC -u CXX -u
  CGO_LDFLAGS GOOS=linux GOARCH=amd64 go install ...` for host-side tools.
- **When `Builds:` has more than one entry, F-Droid's CI builds every one
  of them in the same job/VM/`$HOME`, back to back** — anything that
  extracts an archive with a tool that prompts on a file collision (`unzip`
  without `-o`) hangs on stdin (no TTY) and fails the *second* build with
  a confusing "Could not build app ... Error running build command",
  no matter how correct that build's own steps are in isolation. Always
  pass the force/overwrite flag (`unzip -q -o`, not just `-q`) on anything
  that writes into a path a previous entry's steps might have already
  populated (`$HOME/cmdline-tools`, `$HOME/goroot`, etc.) — `tar` already
  overwrites by default, this only bit `unzip`.

## IzzyOnDroid

- Request: **https://codeberg.org/IzzyOnDroid/repodata/issues/435**
- Draft text (for reference / future re-submission if needed):
  `docs/fdroid/izzyondroid-rfp-draft.md`
- Model is completely different from official F-Droid: IzzyOnDroid does not
  build from source, it repackages our own signed release APK. Because the
  asset name (`USBridgeClient-Android-arm64-market.apk`) is stable across
  every release (see `client/scripts/build_android_gradle.sh`), their
  update-checker can, in principle, be pointed at
  `.../releases/latest/download/USBridgeClient-Android-arm64-market.apk` and
  pick up new versions automatically without us doing anything per-release
  — no manual MR needed the way official F-Droid requires. Confirm this is
  actually how they've configured it once/if the app is accepted.
