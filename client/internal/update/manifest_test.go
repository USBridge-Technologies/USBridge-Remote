package update

import "testing"

func TestAssetURLForTag(t *testing.T) {
	if got, want := assetURL("test-1", "m.json"), "https://github.com/"+repoOwner+"/"+repoName+"/releases/download/test-1/m.json"; got != want {
		t.Fatalf("assetURL(tag) = %q, want %q", got, want)
	}
	if got, want := assetURL("", "m.json"), releaseAssetURL("m.json"); got != want {
		t.Fatalf("assetURL(\"\") = %q, want %q", got, want)
	}
}
