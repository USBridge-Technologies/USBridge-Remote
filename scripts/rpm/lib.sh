# Shared RPM packaging for usbridge-agent and usbridge-client (Fedora, RHEL
# and its rebuilds); sourced by agent/scripts/build_rpm.sh and
# client/scripts/build_rpm.sh. The RPM counterpart of ../deb/lib.sh.
#
# Both packages point dnf at one repository, [usbridge], signed with the
# same GPG key as the APT index. dnf needs a repodata/ directory, which flat
# release assets can't provide, so release-all publishes only the metadata
# on the rpm-repo branch (served by raw.githubusercontent.com) and points it
# at the .rpm assets of that release.
#
# Unlike dpkg, rpm lets two packages own the same file when both ship it
# identically, so the repo file and the key are plain package files: they
# stay while either package is installed. The repo file is
# %config(noreplace), so enabled=0 in it survives upgrades.

RPM_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Both overridable only to test the update flow against a local repository
# signed with a throwaway key; releases always use these defaults.
RPM_REPO_URL="${RPM_REPO_URL:-https://raw.githubusercontent.com/USBridge-Technologies/USBridge-Remote/rpm-repo/}"
RPM_KEYRING_SRC="${RPM_KEYRING_SRC:-$RPM_LIB_DIR/../deb/usbridge-archive-keyring.gpg}"
RPM_PACKAGES="usbridge-agent usbridge-client"
RPM_KEY=/etc/pki/rpm-gpg/RPM-GPG-KEY-usbridge
RPM_REPO_FILE=/etc/yum.repos.d/usbridge.repo

# rpm_add_repo ROOT LIBDIR: stages the repo file, the key (armored, which
# rpm --import wants) and the dnf-upgrade helper (LIBDIR/dnf-upgrade, run as
# root through pkexec by the app's update dialog).
rpm_add_repo() {
    local root="$1" libdir="$2" gnupg
    mkdir -p "$root$libdir" "$root$(dirname "$RPM_KEY")" "$root$(dirname "$RPM_REPO_FILE")"
    gnupg="$(mktemp -d)"
    gpg --homedir "$gnupg" --batch --quiet --import "$RPM_KEYRING_SRC" 2>/dev/null
    gpg --homedir "$gnupg" --batch --armor --export > "$root$RPM_KEY"
    rm -rf "$gnupg"
    grep -q 'BEGIN PGP PUBLIC KEY BLOCK' "$root$RPM_KEY" || { echo "no key in $RPM_KEYRING_SRC" >&2; return 1; }
    chmod 0644 "$root$RPM_KEY"

    cat > "$root$RPM_REPO_FILE" <<EOF
# USBridge updates (usbridge-agent, usbridge-client). Set enabled=0 to stop them.
[usbridge]
name=USBridge
baseurl=$RPM_REPO_URL
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=file://$RPM_KEY
EOF
    chmod 0644 "$root$RPM_REPO_FILE"

    cat > "$root$libdir/dnf-upgrade" <<EOF
#!/bin/sh
# Upgrades the installed USBridge packages from the USBridge repository
# only. Run as root through pkexec by the app's update dialog.
set -eu
pkgs=
for p in $RPM_PACKAGES; do
    if rpm -q --quiet "\$p"; then pkgs="\$pkgs \$p"; fi
done
exec dnf -y --repo=usbridge --refresh upgrade \$pkgs
EOF
    chmod 0755 "$root$libdir/dnf-upgrade"
}

# rpm_glibc ROOT: "libc.so.6(GLIBC_X.Y)(64bit)" for the newest GLIBC_ symbol
# version any ELF file under ROOT needs, so dnf refuses an older system up
# front instead of installing something that fails to start.
rpm_glibc() {
    local v
    v="$(find "$1" -type f -exec sh -c 'head -c4 "$1" | grep -q ELF && objdump -T "$1" 2>/dev/null' _ {} \; \
        | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/GLIBC_//' | sort -uV | tail -1)"
    echo "libc.so.6(${v:+GLIBC_$v})(64bit)"
}

# rpm_build ROOT OUTDIR PKG VERSION REQUIRES RECOMMENDS SUMMARY DESCRIPTION
# REQUIRES and RECOMMENDS are comma-separated; "libc" in REQUIRES becomes
# rpm_glibc's minimum. Writes OUTDIR/PKG-VERSION-1.x86_64.rpm. Dependencies
# are only these: the bundled libraries under /opt must not turn into
# automatic Requires/Provides.
rpm_build() {
    local root="$1" outdir="$2" pkg="$3" version="$4" requires="$5" recommends="$6" summary="$7" description="$8"
    local top spec f
    requires="$(printf '%s' "$requires" | sed "s/^libc\(,\|$\)/$(rpm_glibc "$root")\1/")"
    top="$(mktemp -d)"
    spec="$top/$pkg.spec"
    {
        echo "Name: $pkg"
        echo "Version: $version"
        echo "Release: 1"
        echo "Summary: $summary"
        echo "License: GPL-3.0-only"
        echo "URL: https://www.usbridge.io"
        echo "Vendor: USBridge Technologies"
        echo "Packager: USBridge Technologies <support@usbridge.io>"
        echo "AutoReqProv: no"
        echo "Requires: $requires"
        [[ -n "$recommends" ]] && echo "Recommends: $recommends"
        echo
        echo "%description"
        printf '%s\n' "$description"
        echo
        echo "%install"
        echo "cp -a '$root/.' %{buildroot}/"
        echo
        echo "%files"
        echo "%defattr(-,root,root,-)"
        (cd "$root" && find . -mindepth 1 \( -type f -o -type l \) -printf '/%P\n' | LC_ALL=C sort) | while read -r f; do
            case "$f" in
                "$RPM_REPO_FILE") echo "%config(noreplace) \"$f\"" ;;
                *) echo "\"$f\"" ;;
            esac
        done
        # Own the package's own directories, not shared ones like /usr/bin.
        (cd "$root" && find . -mindepth 1 -type d -printf '/%P\n') | grep -E '^/(usr/lib|opt)/usbridge-' | sed 's/^/%dir "/; s/$/"/'
    } > "$spec"

    rpmbuild -bb --quiet "$spec" \
        --define "_topdir $top" \
        --define "_rpmdir $top/out" \
        --define "_build_id_links none" \
        --define "__os_install_post %{nil}" \
        --define "debug_package %{nil}" \
        --define "_binary_payload w19T0.zstdio" \
        --define "_buildhost usbridge"
    mkdir -p "$outdir"
    mv "$top/out/x86_64/$pkg-$version-1.x86_64.rpm" "$outdir/"
    rm -rf "$top"
    echo "rpm: $outdir/$pkg-$version-1.x86_64.rpm"
}
