package ui

import "testing"

func TestFormatStreamerVersion(t *testing.T) {
	tests := []struct {
		name   string
		app    string
		rust   string
		active bool
		want   string
	}{
		{name: "sunshine", app: "1.2.3", rust: "usbridge-streamer-v9.9.9", active: false, want: "v1.2.3"},
		{name: "sunshine already v", app: "v1.2.3", rust: "", active: false, want: "v1.2.3"},
		{name: "streamer usbridge prefix", app: "1.0.0", rust: "usbridge-streamer-v0.3.16", active: true, want: "v0.3.16"},
		{name: "streamer gamestream prefix", app: "1.0.0", rust: "gamestream-server-v0.3.16", active: true, want: "v0.3.16"},
		{name: "streamer already short", app: "1.0.0", rust: "v0.4.1", active: true, want: "v0.4.1"},
		{name: "streamer empty falls back", app: "2.0.0", rust: "", active: true, want: "v2.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStreamerVersion(tt.app, tt.rust, tt.active)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
