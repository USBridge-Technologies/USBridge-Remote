package gui

import "testing"

func TestParseJoinLink(t *testing.T) {
	id := "2ZLCxukI8F8ZjwhOATClYw"
	if s, err := parseJoinLink("usbridge://join?share=" + id); err != nil || s != id {
		t.Errorf("got %q %v", s, err)
	}
	for _, uri := range []string{
		"usbridge://join",
		"usbridge://join?share=short",
		"usbridge://connect?share=" + id,
		"https://join?share=" + id,
	} {
		if _, err := parseJoinLink(uri); err == nil {
			t.Errorf("%s accepted", uri)
		}
	}
}
