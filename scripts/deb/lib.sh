# Shared .deb packaging for usbridge-agent and usbridge-client; sourced by
# agent/scripts/build_deb.sh and client/scripts/build_deb.sh.
#
# Both packages point apt at one flat repository: the APT index release-all
# publishes as assets of the latest GitHub release. Two packages can't ship
# the same file, so like Chrome's .deb the source list and keyring are
# written by postinst on first install rather than shipped, and removed on
# purge only once neither package is left.

DEB_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

DEB_REPO_URL="https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download"
DEB_LIST="/etc/apt/sources.list.d/usbridge.list"
DEB_KEYRING="/usr/share/keyrings/usbridge-archive-keyring.gpg"
DEB_PACKAGES="usbridge-agent usbridge-client"

# deb_add_repo ROOT PKG LIBDIR: stages the keyring, the apt-upgrade helper
# (LIBDIR/apt-upgrade, run as root through pkexec by the app's update
# dialog) and the maintainer scripts. LIBDIR is the install path, e.g.
# /usr/lib/usbridge-agent.
deb_add_repo() {
    local root="$1" pkg="$2" libdir="$3"
    mkdir -p "$root/DEBIAN" "$root$libdir"
    install -m 0644 "$DEB_LIB_DIR/usbridge-archive-keyring.gpg" "$root$libdir/archive-keyring.gpg"

    cat > "$root$libdir/apt-upgrade" <<EOF
#!/bin/sh
# Upgrades the installed USBridge packages from the USBridge APT repository
# only. Run as root through pkexec by the app's update dialog.
set -eu
export DEBIAN_FRONTEND=noninteractive
apt-get update -o Dir::Etc::sourcelist=$DEB_LIST -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0
apt-get install -y --only-upgrade -o Dpkg::Options::=--force-confold $DEB_PACKAGES
EOF
    chmod 0755 "$root$libdir/apt-upgrade"

    cat > "$root/DEBIAN/postinst" <<EOF
#!/bin/sh
set -e
if [ "\$1" = configure ]; then
    install -m 0644 $libdir/archive-keyring.gpg $DEB_KEYRING
    # First install only: an upgrade never brings back a list the user removed.
    if [ -z "\${2:-}" ] && [ ! -e $DEB_LIST ]; then
        cat > $DEB_LIST <<'LIST'
# USBridge updates (usbridge-agent, usbridge-client). Delete this file to stop them.
deb [arch=amd64 signed-by=$DEB_KEYRING] $DEB_REPO_URL ./
LIST
    fi
fi
EOF
    cat > "$root/DEBIAN/postrm" <<EOF
#!/bin/sh
set -e
if [ "\$1" = purge ]; then
    for p in $DEB_PACKAGES; do
        [ "\$p" = $pkg ] && continue
        if dpkg-query -W -f='\${Status}' "\$p" 2>/dev/null | grep -q 'ok installed'; then
            exit 0
        fi
    done
    rm -f $DEB_LIST $DEB_KEYRING
fi
EOF
    chmod 0755 "$root/DEBIAN/postinst" "$root/DEBIAN/postrm"
}

# deb_libc6 ROOT: "libc6 (>= X.Y)" for the newest GLIBC_ symbol version any
# ELF file under ROOT needs. The binaries are built against the build
# machine's glibc, so apt should refuse an older system up front instead of
# installing something that fails to start.
deb_libc6() {
    local v
    v="$(find "$1" -type f -exec sh -c 'head -c4 "$1" | grep -q ELF && objdump -T "$1" 2>/dev/null' _ {} \; \
        | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/GLIBC_//' | sort -uV | tail -1)"
    echo "libc6${v:+ (>= $v)}"
}

# deb_build ROOT OUT PKG VERSION DEPENDS RECOMMENDS SECTION SUMMARY DESCRIPTION
# A "libc6" entry in DEPENDS gets its minimum version from deb_libc6.
deb_build() {
    local root="$1" out="$2" pkg="$3" version="$4" depends="$5" recommends="$6" section="$7" summary="$8" description="$9"
    mkdir -p "$root/usr/share/doc/$pkg"
    cat > "$root/usr/share/doc/$pkg/copyright" <<EOF
Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: $pkg
Source: https://github.com/USBridge-Technologies/USBridge-Remote
EOF
    depends="$(printf '%s' "$depends" | sed "s/^libc6\(,\|$\)/$(deb_libc6 "$root")\1/")"
    local size_kb
    size_kb="$(du -sk --exclude=DEBIAN "$root" | cut -f1)"
    {
        echo "Package: $pkg"
        echo "Version: $version"
        echo "Architecture: amd64"
        echo "Maintainer: USBridge Technologies <support@usbridge.io>"
        echo "Installed-Size: $size_kb"
        echo "Depends: $depends"
        [ -n "$recommends" ] && echo "Recommends: $recommends"
        echo "Section: $section"
        echo "Priority: optional"
        echo "Homepage: https://www.usbridge.io"
        echo "Description: $summary"
        printf '%s\n' "$description" | sed 's/^/ /'
    } > "$root/DEBIAN/control"
    rm -f "$out"
    dpkg-deb --root-owner-group -Zxz --build "$root" "$out"
    echo "deb: $out"
}
