import os
import sys

# Replace DIST_WIN_DLLS and DIST_WIN_BIN
with open('scripts/build_windows.sh', 'r', encoding='utf-8') as f:
    c = f.read()

c = c.replace('DIST_WIN_DLLS="$DIST_WIN"\nDIST_WIN_BIN="$DIST_WIN"', 'DIST_WIN_DLLS="$DIST_WIN/bin"\nDIST_WIN_BIN="$DIST_WIN/bin"')
c = c.replace('cp "$BUILD_CACHE_APP_EXE" "$DIST_WIN/$APP_EXE_NAME"', 'cp "$BUILD_CACHE_APP_EXE" "$DIST_WIN_BIN/$APP_EXE_NAME"')

# Add VBS script generation for shortcut
vbs_injection = """
cp "$BUILD_CACHE_APP_EXE" "$DIST_WIN_BIN/$APP_EXE_NAME"

# Create a relative shortcut using explorer.exe
echo "Creating shortcut..."
cat << 'VBS_EOF' > "$DIST_WIN/make_shortcut.vbs"
Set oWS = WScript.CreateObject("WScript.Shell")
sLinkFile = WScript.Arguments(0)
Set oLink = oWS.CreateShortcut(sLinkFile)
oLink.TargetPath = "explorer.exe"
oLink.Arguments = "bin\\" & WScript.Arguments(1)
oLink.IconLocation = oWS.CurrentDirectory & "\\bin\\" & WScript.Arguments(1) & ", 0"
oLink.WindowStyle = 1
oLink.Save
VBS_EOF
cscript //nologo "$DIST_WIN/make_shortcut.vbs" "$(cygpath -w "$DIST_WIN/USBridge_Client.lnk")" "$APP_EXE_NAME"
rm -f "$DIST_WIN/make_shortcut.vbs"
"""

c = c.replace('cp "$BUILD_CACHE_APP_EXE" "$DIST_WIN_BIN/$APP_EXE_NAME"', vbs_injection)

with open('scripts/build_windows.sh', 'w', encoding='utf-8') as f:
    f.write(c)

print("Updated build script")
