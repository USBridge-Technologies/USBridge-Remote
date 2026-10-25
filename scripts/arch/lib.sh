# Shared Arch Linux packaging for usbridge-agent and usbridge-client; sourced
# by agent/scripts/build_arch.sh and client/scripts/build_arch.sh. The Arch
# counterpart of ../deb/lib.sh.
#
# Both packages point pacman at one repository, [usbridge]: the pacman
# database release-all publishes as assets of the latest GitHub release,
# signed with the same GPG key as the APT index. As with the .deb, two
# packages can't ship the same file, so the keyring and the pacman.conf
# section are written by the install script on first install rather than
# shipped, and removed only once neither package is left.
#
# Packages are assembled with bsdtar + zstd the way makepkg does, so they
# build on the Ubuntu release runners next to the .deb, from the same
# binaries.

ARCH_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$ARCH_LIB_DIR/../polkit.sh"

ARCH_REPO=usbridge
# Both overridable only to test the update flow against a local repository
# signed with a throwaway key; releases always use these defaults.
ARCH_REPO_URL="${ARCH_REPO_URL:-https://github.com/USBridge-Technologies/USBridge-Remote/releases/latest/download}"
ARCH_KEYRING_SRC="${ARCH_KEYRING_SRC:-$ARCH_LIB_DIR/../deb/usbridge-archive-keyring.gpg}"
ARCH_PACKAGES="usbridge-agent usbridge-client"

# arch_add_repo ROOT PKG LIBDIR: stages the keyring, the pacman-upgrade
# helper (LIBDIR/pacman-upgrade, run as root through pkexec by the app's
# update dialog) and the install script. LIBDIR is the install path, e.g.
# /usr/lib/usbridge-agent; the other package's LIBDIR tells post_remove
# whether it's still installed.
arch_add_repo() {
    local root="$1" pkg="$2" libdir="$3" fpr others="" p
    mkdir -p "$root$libdir"
    install -m 0644 "$ARCH_KEYRING_SRC" "$root$libdir/archive-keyring.gpg"
    fpr="$(gnupg="$(mktemp -d)"; gpg --homedir "$gnupg" --batch --with-colons --show-keys "$ARCH_KEYRING_SRC" 2>/dev/null \
        | awk -F: '/^fpr/{print $10; exit}'; rm -rf "$gnupg")"
    [[ -n "$fpr" ]] || { echo "no key in $ARCH_KEYRING_SRC" >&2; return 1; }
    for p in $ARCH_PACKAGES; do
        [[ "$p" = "$pkg" ]] || others="$others $(arch_libdir "$p")"
    done

    cat > "$root$libdir/pacman-upgrade" <<EOF
#!/bin/sh
# Upgrades the installed USBridge packages from the USBridge pacman
# repository only. Run as root through pkexec by the app's update dialog.
# A config holding just [$ARCH_REPO] refreshes only its database, so this
# never leaves the rest of the system partially upgraded.
set -eu
conf="\$(mktemp)"
trap 'rm -f "\$conf"' EXIT
cat > "\$conf" <<'CONF'
[options]
Architecture = auto
[$ARCH_REPO]
SigLevel = Required
Server = $ARCH_REPO_URL
CONF
pacman --config "\$conf" -Sy --noconfirm --needed \$(pacman -Qq $ARCH_PACKAGES 2>/dev/null)
EOF
    chmod 0755 "$root$libdir/pacman-upgrade"
    polkit_add_policy "$root" "$libdir" pacman-upgrade

    cat > "$root/.INSTALL" <<EOF
_keyrings=/usr/share/pacman/keyrings

_add_key() {
    install -Dm644 $libdir/archive-keyring.gpg \$_keyrings/$ARCH_REPO.gpg
    echo "$fpr:4:" > \$_keyrings/$ARCH_REPO-trusted
    pacman-key --populate $ARCH_REPO >/dev/null 2>&1 ||
        echo "warning: could not add the USBridge signing key; run: pacman-key --populate $ARCH_REPO"
}

post_install() {
    _add_key
    if ! grep -q '^\[$ARCH_REPO\]' /etc/pacman.conf; then
        cat >> /etc/pacman.conf <<'CONF'

# USBridge updates (usbridge-agent, usbridge-client). Delete this section to stop them.
[$ARCH_REPO]
SigLevel = Required
Server = $ARCH_REPO_URL
CONF
    fi
}

post_upgrade() {
    _add_key
}

post_remove() {
    for d in$others; do
        [ -e "\$d" ] && return 0
    done
    # Drops our comment and the [$ARCH_REPO] section, up to the next section
    # or blank line.
    _conf="\$(mktemp)"
    awk '/^# USBridge updates/ { next }
         /^\[$ARCH_REPO\]/ { skip = 1; next }
         skip && (/^\[/ || /^[[:space:]]*\$/) { skip = 0 }
         !skip' /etc/pacman.conf > "\$_conf" && cat "\$_conf" > /etc/pacman.conf
    rm -f "\$_conf"
    rm -f \$_keyrings/$ARCH_REPO.gpg \$_keyrings/$ARCH_REPO-trusted
}
EOF
}

# arch_libdir PKG: where PKG installs (matches the build_*.sh LIBDIR).
arch_libdir() {
    case "$1" in
        usbridge-agent) echo /usr/lib/usbridge-agent ;;
        usbridge-client) echo /opt/usbridge-client ;;
    esac
}

# arch_build ROOT OUTDIR PKG VERSION DEPENDS OPTDEPENDS SUMMARY
# DEPENDS is space-separated; OPTDEPENDS is newline-separated "pkg: reason".
# Writes OUTDIR/PKG-VERSION-1-x86_64.pkg.tar.zst: the versioned name stays
# in the repository database, so pacman's package cache never confuses two
# releases.
arch_build() {
    local root="$1" outdir="$2" pkg="$3" version="$4" depends="$5" optdepends="$6" summary="$7"
    local out="$outdir/$pkg-$version-1-x86_64.pkg.tar.zst" d size
    size="$(du -sb --exclude=.PKGINFO --exclude=.INSTALL --exclude=.MTREE "$root" | cut -f1)"
    {
        echo "pkgname = $pkg"
        echo "pkgbase = $pkg"
        echo "xdata = pkgtype=pkg"
        echo "pkgver = $version-1"
        echo "pkgdesc = $summary"
        echo "url = https://www.usbridge.io"
        echo "builddate = $(date +%s)"
        echo "packager = USBridge Technologies <support@usbridge.io>"
        echo "size = $size"
        echo "arch = x86_64"
        echo "license = GPL-3.0-only"
        for d in $depends; do echo "depend = $d"; done
        [[ -n "$optdepends" ]] && printf '%s\n' "$optdepends" | sed 's/^/optdepend = /'
    } > "$root/.PKGINFO"

    # makepkg's layout: .MTREE, .PKGINFO, .INSTALL first, every entry owned
    # by root whoever runs the build.
    local own=(--uid 0 --gid 0 --uname root --gname root)
    arch_files() {
        printf '%s\0' .PKGINFO
        [[ -f .INSTALL ]] && printf '%s\0' .INSTALL
        find . -mindepth 1 ! -name .PKGINFO ! -name .INSTALL ! -name .MTREE -printf '%P\0' | LC_ALL=C sort -z
    }
    (
        cd "$root"
        rm -f .MTREE
        arch_files | LANG=C bsdtar -cnf - "${own[@]}" --format=mtree \
            --options='!all,use-set,type,uid,gid,mode,time,size,md5,sha256,link' \
            --null --files-from - | gzip -c -f -n > .MTREE
        { printf '%s\0' .MTREE; arch_files; } | LANG=C bsdtar -cnf - "${own[@]}" --null --files-from - \
            | zstd -c -T0 -19 -q > "$out"
    )
    echo "pkg: $out"
}
