# Generates internal/input/remote_layouts_gen.go: for each Windows keyboard
# layout below, which physical key (PS/2 Set-1 scan code) and modifiers type
# each character. Run on Windows (any edition ships every kbd*.dll):
#   powershell -ExecutionPolicy Bypass -File tools\gen_keyboard_layouts.ps1 -Out internal\input\remote_layouts_gen.go
param([Parameter(Mandatory = $true)][string]$Out)

$ErrorActionPreference = 'Stop'

# KLID and display name (the client's layout menu shows the name).
$layouts = @(
    @('00000409', 'English (US)'),
    @('00020409', 'English (US International)'),
    @('00000809', 'English (UK)'),
    @('0000040E', 'Hungarian'),
    @('0001040E', 'Hungarian (101-key)'),
    @('00000407', 'German'),
    @('00000807', 'German (Switzerland)'),
    @('0000040C', 'French'),
    @('0000080C', 'French (Belgium)'),
    @('0000100C', 'French (Switzerland)'),
    @('00001009', 'French (Canada)'),
    @('00000410', 'Italian'),
    @('0000040A', 'Spanish'),
    @('0000080A', 'Spanish (Latin America)'),
    @('00000816', 'Portuguese'),
    @('00000416', 'Portuguese (Brazil)'),
    @('00000413', 'Dutch'),
    @('00000406', 'Danish'),
    @('00000414', 'Norwegian'),
    @('0000041D', 'Swedish'),
    @('0000040B', 'Finnish'),
    @('0000040F', 'Icelandic'),
    @('00000415', 'Polish (Programmers)'),
    @('00000405', 'Czech'),
    @('0000041B', 'Slovak'),
    @('00000424', 'Slovenian'),
    @('0000041A', 'Croatian'),
    @('00010418', 'Romanian'),
    @('0000041F', 'Turkish Q'),
    @('00000425', 'Estonian'),
    @('00000426', 'Latvian'),
    @('00000427', 'Lithuanian'),
    @('00000408', 'Greek'),
    @('00000419', 'Russian'),
    @('00000422', 'Ukrainian'),
    @('00000423', 'Belarusian'),
    @('00000402', 'Bulgarian'),
    @('0000043F', 'Kazakh')
)

Add-Type @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;

public static class KbdGen {
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern IntPtr LoadKeyboardLayoutW(string klid, uint flags);
    [DllImport("user32.dll")] static extern bool UnloadKeyboardLayout(IntPtr hkl);
    [DllImport("user32.dll")] static extern int GetKeyboardLayoutList(int n, IntPtr[] list);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern short VkKeyScanExW(char ch, IntPtr hkl);
    [DllImport("user32.dll")] static extern uint MapVirtualKeyExW(uint code, uint mapType, IntPtr hkl);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int ToUnicodeEx(uint vk, uint scan, byte[] state, StringBuilder buf, int n, uint flags, IntPtr hkl);

    static bool CharacterKey(uint sc) {
        return (sc >= 0x02 && sc <= 0x0D) || (sc >= 0x10 && sc <= 0x1B) || (sc >= 0x1E && sc <= 0x29) ||
               (sc >= 0x2B && sc <= 0x35) || sc == 0x39 || sc == 0x56;
    }

    // ToUnicodeEx keeps dead-key state in the layout; a second call with a
    // space flushes it so the next probe starts clean.
    static int Probe(uint vk, uint sc, bool shift, bool altgr, IntPtr hkl, StringBuilder buf) {
        var state = new byte[256];
        if (shift) { state[0x10] = 0x80; state[0xA0] = 0x80; }
        if (altgr) { state[0x11] = 0x80; state[0xA2] = 0x80; state[0x12] = 0x80; state[0xA5] = 0x80; }
        buf.Clear();
        int n = ToUnicodeEx(vk, sc, state, buf, buf.Capacity, 0x4, hkl);
        var flush = new StringBuilder(8);
        ToUnicodeEx(0x20, 0x39, new byte[256], flush, 8, 0x4, hkl);
        ToUnicodeEx(0x20, 0x39, new byte[256], flush, 8, 0x4, hkl);
        return n;
    }

    // Entries: rune<<16 | flags<<8 | scan; flags 1=Shift 2=AltGr 4=dead key.
    public static List<uint> Table(string klid) {
        var before = new IntPtr[64];
        int nBefore = GetKeyboardLayoutList(64, before);
        IntPtr hkl = LoadKeyboardLayoutW(klid, 0x80 /* KLF_NOTELLSHELL */);
        if (hkl == IntPtr.Zero) throw new Exception("LoadKeyboardLayout " + klid + " failed");
        var outList = new List<uint>();
        var buf = new StringBuilder(8);
        try {
            for (int c = 0x20; c < 0x3000; c++) {
                if (c >= 0xD800 && c < 0xE000) continue;
                short r = VkKeyScanExW((char)c, hkl);
                if (r == -1) continue;
                uint vk = (uint)(r & 0xFF);
                int st = (r >> 8) & 0xFF;
                bool shift = (st & 1) != 0, ctrl = (st & 2) != 0, alt = (st & 4) != 0;
                if (ctrl != alt || (st & ~7) != 0) continue; // Ctrl-only = control characters
                bool altgr = ctrl && alt;
                uint sc = MapVirtualKeyExW(vk, 0 /* MAPVK_VK_TO_VSC */, hkl);
                if (!CharacterKey(sc)) continue;
                int n = Probe(vk, sc, shift, altgr, hkl, buf);
                bool dead = n < 0;
                if (n == 0 || (n > 0 && (buf.Length < 1 || buf[0] != (char)c))) continue;
                uint flags = (shift ? 1u : 0) | (altgr ? 2u : 0) | (dead ? 4u : 0);
                outList.Add(((uint)c << 16) | (flags << 8) | sc);
            }
        } finally {
            bool preloaded = false;
            for (int i = 0; i < nBefore; i++) if (before[i] == hkl) preloaded = true;
            if (!preloaded) UnloadKeyboardLayout(hkl);
        }
        return outList;
    }
}
'@

$sb = [System.Text.StringBuilder]::new()
[void]$sb.AppendLine('// Code generated by tools/gen_keyboard_layouts.ps1 from the Windows keyboard')
[void]$sb.AppendLine('// layouts; DO NOT EDIT.')
[void]$sb.AppendLine('')
[void]$sb.AppendLine('package input')
[void]$sb.AppendLine('')
[void]$sb.AppendLine('var remoteLayoutTables = []remoteLayoutTable{')
foreach ($l in $layouts) {
    $entries = [KbdGen]::Table($l[0])
    Write-Host ("{0} {1}: {2} characters" -f $l[0], $l[1], $entries.Count)
    [void]$sb.AppendLine(("`t{{ID: ""{0}"", Name: ""{1}"", entries: []uint32{{" -f $l[0], $l[1]))
    for ($i = 0; $i -lt $entries.Count; $i += 8) {
        $chunk = $entries[$i..([Math]::Min($i + 7, $entries.Count - 1))] | ForEach-Object { '0x{0:x8}' -f $_ }
        [void]$sb.AppendLine("`t`t" + ($chunk -join ', ') + ',')
    }
    [void]$sb.AppendLine("`t}},")
}
[void]$sb.AppendLine('}')
[System.IO.File]::WriteAllText($Out, $sb.ToString().Replace("`r`n", "`n"), [System.Text.UTF8Encoding]::new($false))
