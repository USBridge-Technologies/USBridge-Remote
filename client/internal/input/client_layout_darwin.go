//go:build darwin

package input

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>

// activeLayoutSourceID copies the ID of the keyboard layout macOS types with
// ("com.apple.keylayout.Hungarian") into buf. HIToolbox keeps it in
// preferences, which unlike the Text Input Sources API is safe off the main
// thread.
static int activeLayoutSourceID(char *buf, int n) {
	CFStringRef app = CFSTR("com.apple.HIToolbox");
	CFPreferencesAppSynchronize(app);
	CFTypeRef v = CFPreferencesCopyAppValue(CFSTR("AppleCurrentKeyboardLayoutInputSourceID"), app);
	if (v == NULL) return 0;
	int ok = CFGetTypeID(v) == CFStringGetTypeID() &&
		CFStringGetCString((CFStringRef)v, buf, n, kCFStringEncodingUTF8);
	CFRelease(v);
	return ok;
}
*/
import "C"

import "strings"

// ClientKeyboardLayoutID is the Windows KLID of the layout this Mac types
// with, so a hardware KVM target gets the keys that produce the same
// characters; "" for an unknown one (auto then means US).
func ClientKeyboardLayoutID() string {
	var buf [256]C.char
	if C.activeLayoutSourceID(&buf[0], C.int(len(buf))) == 0 {
		return ""
	}
	name := strings.TrimPrefix(C.GoString(&buf[0]), "com.apple.keylayout.")
	return macLayoutKLIDs[name]
}

// macLayoutKLIDs maps macOS layout names to Windows KLIDs.
var macLayoutKLIDs = map[string]string{
	"ABC": "00000409", "US": "00000409", "USExtended": "00000409", "USInternational-PC": "00020409",
	"British": "00000809", "British-PC": "00000809", "Irish": "00001809",
	"Hungarian": "0000040E", "Hungarian-QWERTY": "0000040E",
	"German": "00000407", "Austrian": "00000407", "Swiss": "00000807", "SwissFrench": "0000100C", "SwissGerman": "00000807",
	"French": "0000040C", "French-PC": "0000040C", "Belgian": "0000080C",
	"Canadian-CSA": "00001009", "Canadian": "00001009",
	"Italian": "00000410", "Italian-Pro": "00000410",
	"Spanish": "0000040A", "Spanish-ISO": "0000040A", "LatinAmerican": "0000080A",
	"Portuguese": "00000816", "Brazilian": "00000416", "Brazilian-ABNT2": "00000416",
	"Dutch": "00000413", "Danish": "00000406", "Norwegian": "00000414", "Swedish": "0000041D", "Swedish-Pro": "0000041D",
	"Finnish": "0000040B", "Icelandic": "0000040F", "Polish": "00000415", "PolishPro": "00000415",
	"Czech": "00000405", "Czech-QWERTY": "00010405", "Slovak": "0000041B", "Slovak-QWERTY": "0001041B",
	"Slovenian": "00000424", "Croatian": "0000041A", "Romanian": "00010418", "Turkish": "0000041F", "Turkish-QWERTY": "0000041F",
	"Estonian": "00000425", "Latvian": "00000426", "Lithuanian": "00010427",
	"Greek": "00000408", "Russian": "00000419", "RussianWin": "00000419", "Ukrainian": "00000422", "Ukrainian-PC": "00000422",
	"Belarusian": "00000423", "Bulgarian": "00000402", "Kazakh": "0000043F",
}
