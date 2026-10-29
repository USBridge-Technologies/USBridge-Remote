#!/usr/bin/env bash
# Publishes the dnf repository for one GitHub release: the repodata/ for the
# release's usbridge-*.rpm assets, signed with the APT key, as one
# force-pushed commit on the rpm-repo branch (served by
# raw.githubusercontent.com, see lib.sh). Package locations point at the
# release's own assets, so nothing but the metadata is stored.
#
#   publish_repo.sh TAG RPM_DIR
#
# Needs APT_GPG_PRIVATE_KEY, GH_TOKEN and GITHUB_REPOSITORY in the
# environment, and createrepo_c. Used by release-all.yml's release job and by
# publish-dnf-repo.yml (a pre-release promoted to latest afterwards).
set -euo pipefail

TAG="$1"
RPM_DIR="$2"
: "${APT_GPG_PRIVATE_KEY:?}" "${GH_TOKEN:?}" "${GITHUB_REPOSITORY:?}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
export GNUPGHOME="$work/gnupg"
mkdir -m 700 "$GNUPGHOME"
trap 'rm -rf "$work"' EXIT

printf '%s\n' "$APT_GPG_PRIVATE_KEY" | gpg --batch --import
want="$(gpg --batch --with-colons --show-keys "$SCRIPT_DIR/../deb/usbridge-archive-keyring.gpg" | awk -F: '/^fpr/{print $10; exit}')"
gpg --batch --with-colons --list-secret-keys "$want" >/dev/null

repo="$work/rpm-repo"
mkdir -p "$repo/pkgs"
cp "$RPM_DIR"/usbridge-*.rpm "$repo/pkgs/"
ls "$repo/pkgs"
createrepo_c --baseurl "https://github.com/$GITHUB_REPOSITORY/releases/download/$TAG/" \
    --outputdir "$repo" "$repo/pkgs"
rm -rf "$repo/pkgs"
gpg --batch --yes --local-user "$want" --detach-sign --armor -o "$repo/repodata/repomd.xml.asc" "$repo/repodata/repomd.xml"

cd "$repo"
git init -q -b rpm-repo
git add repodata
git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
    commit -q -m "dnf repository for $TAG"
git push -q --force "https://x-access-token:$GH_TOKEN@github.com/$GITHUB_REPOSITORY.git" rpm-repo
echo "dnf repository now serves $TAG"
