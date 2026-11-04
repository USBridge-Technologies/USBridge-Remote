# USB passthrough setup run as root by the agent's .deb, .rpm and Arch
# packages (inlined into postinst/%post/.INSTALL by build_deb.sh,
# build_rpm.sh and build_arch.sh), so a fresh install needs no "Grant" or
# "Install USB Driver" click in the agent:
#
#   - the passwordless usbip attach/detach polkit rule, byte for byte the
#     one the agent's own grant installs (internal/usbpass/usbip-polkit.rules,
#     shipped as $USBRIDGE_LIBDIR/usbip-polkit.rules);
#   - the usbridge-usbip group, with every regular user (uid 1000-59999
#     with a login shell) and the user who ran the install in it -- polkit
#     reads the group from the user database, so no re-login is needed;
#   - vhci-hcd loaded now and on every boot.
#
# The usbip tool itself is a Recommends (deb, rpm) / optdepends (Arch) of
# the package; where it's missing the agent installs it on first start.
# POSIX sh: Arch's .INSTALL and rpm scriptlets run under /bin/sh.
# Never fails the package install.

USBRIDGE_LIBDIR=/usr/lib/usbridge-agent
USBRIDGE_USBIP_GROUP=usbridge-usbip
USBRIDGE_USBIP_RULE=/etc/polkit-1/rules.d/49-usbridge-usbip.rules
USBRIDGE_VHCI_CONF=/etc/modules-load.d/usbridge-vhci-hcd.conf

usbridge_usbip_configure() {
    getent group $USBRIDGE_USBIP_GROUP >/dev/null 2>&1 ||
        groupadd -r $USBRIDGE_USBIP_GROUP 2>/dev/null ||
        groupadd $USBRIDGE_USBIP_GROUP 2>/dev/null || true
    if getent group $USBRIDGE_USBIP_GROUP >/dev/null 2>&1; then
        _usbridge_users="$(getent passwd | awk -F: '$3 >= 1000 && $3 < 60000 && $7 !~ /(nologin|false)$/ { print $1 }')"
        [ -n "${SUDO_USER:-}" ] && _usbridge_users="$_usbridge_users $SUDO_USER"
        [ -n "${PKEXEC_UID:-}" ] && _usbridge_users="$_usbridge_users $(getent passwd "$PKEXEC_UID" | cut -d: -f1)"
        for _u in $_usbridge_users; do
            [ "$_u" = root ] && continue
            usermod -aG $USBRIDGE_USBIP_GROUP "$_u" 2>/dev/null || true
        done
    fi
    if [ -f $USBRIDGE_LIBDIR/usbip-polkit.rules ]; then
        install -d -m 0755 /etc/polkit-1/rules.d 2>/dev/null &&
            install -m 0644 $USBRIDGE_LIBDIR/usbip-polkit.rules $USBRIDGE_USBIP_RULE 2>/dev/null || true
    fi
    if [ -d /etc/modules-load.d ] || mkdir -p /etc/modules-load.d 2>/dev/null; then
        echo vhci-hcd > $USBRIDGE_VHCI_CONF 2>/dev/null || true
    fi
    modprobe vhci-hcd >/dev/null 2>&1 || true
    return 0
}

usbridge_usbip_remove() {
    rm -f $USBRIDGE_USBIP_RULE $USBRIDGE_VHCI_CONF 2>/dev/null || true
    return 0
}
