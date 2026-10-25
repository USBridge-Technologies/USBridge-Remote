# Shared polkit policy for the update helpers; sourced by deb/lib.sh,
# arch/lib.sh and rpm/lib.sh.

# polkit_add_policy ROOT LIBDIR HELPER: stages a policy naming LIBDIR/HELPER
# (apt-upgrade, pacman-upgrade or dnf-upgrade). pkexec on a bare path asks
# for the admin password every time with a generic prompt; with an
# exec.path policy polkit shows our message and keeps the grant for a while
# (auth_admin_keep), so an update isn't a string of password prompts. The
# action id carries the package name (LIBDIR's basename), so the agent and
# the client don't ship the same file.
polkit_add_policy() {
    local root="$1" libdir="$2" helper="$3" pkg
    pkg="$(basename "$libdir")"
    mkdir -p "$root/usr/share/polkit-1/actions"
    cat > "$root/usr/share/polkit-1/actions/io.usbridge.$pkg.$helper.policy" <<POLICY
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE policyconfig PUBLIC "-//freedesktop//DTD PolicyKit Policy Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/PolicyKit/1/policyconfig.dtd">
<policyconfig>
  <vendor>USBridge Technologies</vendor>
  <action id="io.usbridge.$pkg.$helper">
    <description>Update USBridge</description>
    <message>Authentication is required to update USBridge</message>
    <defaults>
      <allow_any>auth_admin</allow_any>
      <allow_inactive>auth_admin</allow_inactive>
      <allow_active>auth_admin_keep</allow_active>
    </defaults>
    <annotate key="org.freedesktop.policykit.exec.path">$libdir/$helper</annotate>
  </action>
</policyconfig>
POLICY
    chmod 0644 "$root/usr/share/polkit-1/actions/io.usbridge.$pkg.$helper.policy"
}
