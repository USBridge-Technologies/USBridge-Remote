//go:build linux

// Package audio enumerates real system audio output devices ("sinks") so
// the client can pick which one Sunshine captures from — Sunshine owns all
// actual audio capture/streaming as part of GameStream; the agent only
// reports what's available and, via internal/sunshine, tells Sunshine which
// one to use (audio_sink in sunshine.conf).
package audio

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"usbridge_agent/internal/api"
)

type pactlSink struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListSinks returns the real PulseAudio/PipeWire sinks available on this
// host via `pactl`.
func ListSinks() ([]api.AudioSink, error) {
	out, err := exec.Command("pactl", "-f", "json", "list", "sinks").Output()
	if err != nil {
		return nil, fmt.Errorf("pactl list sinks: %w", err)
	}

	var raw []pactlSink
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse pactl output: %w", err)
	}

	defaultSink, _ := DefaultSink()

	sinks := make([]api.AudioSink, 0, len(raw))
	for _, s := range raw {
		sinks = append(sinks, api.AudioSink{
			Name:        s.Name,
			Description: s.Description,
			Default:     s.Name == defaultSink,
		})
	}
	return sinks, nil
}

// DefaultSink returns the system's current default sink name.
func DefaultSink() (string, error) {
	out, err := exec.Command("pactl", "get-default-sink").Output()
	if err != nil {
		return "", fmt.Errorf("pactl get-default-sink: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
