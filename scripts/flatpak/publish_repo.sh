#!/usr/bin/env bash
# Publishes the Flatpak repository for one build: an OSTree repository holding
# the bundle from build_bundle.sh, signed with the APT key, as one force-pushed
# commit on the flatpak-repo branch (served by raw.githubusercontent.com, the
# URL in io.usbridge.Client.flatpakref and usbridge.flatpakrepo). OSTree needs a
# directory tree of objects, which flat release assets can't hold, so unlike
# scripts/rpm/publish_repo.sh the packages themselves are in the branch. The
# branch keeps only the newest build; `flatpak update` needs no older ones.
#
#   publish_repo.sh LABEL BUNDLE.flatpak
#
# Needs APT_GPG_PRIVATE_KEY, GH_TOKEN and GITHUB_REPOSITORY in the environment,
# and flatpak and ostree. Used by release-all.yml's release job and by
# client-flatpak.yml.
set -euo pipefail

LABEL="$1"
BUNDLE="$(realpath "$2")"
: "${APT_GPG_PRIVATE_KEY:?}" "${GH_TOKEN:?}" "${GITHUB_REPOSITORY:?}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
export GNUPGHOME="$work/gnupg"
mkdir -m 700 "$GNUPGHOME"
trap 'rm -rf "$work"' EXIT

printf '%s\n' "$APT_GPG_PRIVATE_KEY" | gpg --batch --import
want="$(gpg --batch --with-colons --show-keys "$SCRIPT_DIR/../deb/usbridge-archive-keyring.gpg" | awk -F: '/^fpr/{print $10; exit}')"
gpg --batch --with-colons --list-secret-keys "$want" >/dev/null

repo="$work/flatpak-repo"
ostree --repo="$repo" init --mode=archive-z2
flatpak build-import-bundle --gpg-sign="$want" "$repo" "$BUNDLE"
flatpak build-update-repo --gpg-sign="$want" --title="USBridge" --default-branch=stable \
    --generate-static-deltas "$repo"

cd "$repo"
# GitHub refuses a file over 100 MB; OSTree's own working directories stay out.
rm -rf tmp state
big="$(find . -type f -size +95M)"
[ -z "$big" ] || { echo "Too big for a git push: $big" >&2; exit 1; }
git init -q -b flatpak-repo
git add -A
git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
    commit -q -m "Flatpak repository for $LABEL"
git push -q --force "https://x-access-token:$GH_TOKEN@github.com/$GITHUB_REPOSITORY.git" flatpak-repo
echo "Flatpak repository now serves $LABEL"
