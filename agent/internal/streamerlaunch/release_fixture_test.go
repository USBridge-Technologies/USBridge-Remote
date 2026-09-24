package streamerlaunch

import (
	"os"
	"testing"
)

// testdata/release-v0.3.86-* is a real manifest signed by rust-shine's
// release CI. It pins that the compiled-in key is the one CI actually
// signs with -- a wrong or rotated-without-updating key would make every
// installed launcher refuse every RustShine build.
func TestRealReleaseManifestVerifies(t *testing.T) {
	m, err := os.ReadFile("testdata/release-v0.3.86-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile("testdata/release-v0.3.86-manifest.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyManifest(m, sig, ReleasePublicKey())
	if err != nil {
		t.Fatalf("real CI-signed manifest rejected: %v", err)
	}
	if got.Version != "usbridge-streamer-v0.3.86" || len(got.Platforms["linux-x86_64"].SHA256) != 64 {
		t.Fatalf("unexpected manifest %+v", got)
	}

	for i := range m {
		bad := append([]byte{}, m...)
		bad[i] ^= 0x01
		if _, err := VerifyManifest(bad, sig, ReleasePublicKey()); err == nil {
			t.Fatalf("manifest with byte %d flipped still verified", i)
		}
	}
}
