//go:build windows && cgo

package service

/*
#include <stdint.h>
*/
import "C"

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Per-present JSONL trace, separate from app.log entirely -- requested live
// 2026-09-19: "log them with the ORIGINAL [wall-clock] time so we can
// correlate/compare, analyze, and precisely trace what's happening" and "is
// it taking many of some frames and few of others, or distributing
// incorrectly" (a distribution/timing question app.log's own throttled,
// human-readable summary lines can't answer -- this writes EVERY actual
// presentation, real or concealed, with a real epoch timestamp, so gaps and
// real/concealed counts can be computed precisely offline instead of
// eyeballed from text). Kept in its own file specifically so it never
// competes with app.log's own throttling/flood concerns -- this one is
// deliberately dense (one line per present, not one line per 2s).
//
// Resolves its own directory rather than importing cmd (package main, not
// importable) -- mirrors cmd/log_paths.go's Windows branch, including the
// same USBRIDGE_LOG_DIR override, so it lands next to app.log without a
// package-layering violation.
func frameSmoothingTraceLogDir() string {
	if d := os.Getenv("USBRIDGE_LOG_DIR"); d != "" {
		return d
	}
	if exePath, err := os.Executable(); err == nil {
		dir := filepath.Dir(exePath)
		if filepath.Base(dir) == "bin" {
			dir = filepath.Dir(dir)
		}
		return filepath.Join(dir, "logs")
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "logs")
	}
	return "logs"
}

type frameSmoothingTraceEntry struct {
	TsUnixMs   int64   `json:"ts_unix_ms"`
	Ts         string  `json:"ts"` // RFC3339 with milliseconds, for human cross-reference
	Kind       string  `json:"kind"` // "real" or "concealed"
	T          float64 `json:"t"`    // extrapolateT; -1 for real frames (not applicable)
	CenterVx   float64 `json:"center_vx"`
	CenterVy   float64 `json:"center_vy"`
	WholeVx    float64 `json:"whole_vx"`
	WholeVy    float64 `json:"whole_vy"`
	NonzeroPct float64 `json:"nonzero_pct"`
	GapMs      float64 `json:"gap_ms"` // wall-clock gap since the previous presented entry
}

var (
	frameSmoothingTraceMu   sync.Mutex
	frameSmoothingTraceFile *os.File
	frameSmoothingTraceEnc  *json.Encoder
	frameSmoothingTraceFail bool // stop retrying every call once open has failed once
)

func frameSmoothingTraceEnsureOpen() bool {
	if frameSmoothingTraceFile != nil {
		return true
	}
	if frameSmoothingTraceFail {
		return false
	}
	dir := frameSmoothingTraceLogDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		frameSmoothingTraceFail = true
		return false
	}
	f, err := os.OpenFile(filepath.Join(dir, "frame_smoothing_trace.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		frameSmoothingTraceFail = true
		return false
	}
	frameSmoothingTraceFile = f
	frameSmoothingTraceEnc = json.NewEncoder(f)
	return true
}

// goFrameSmoothingTraceWrite is vk_render_frame(_vkimage)/vk_render_frame_conceal's
// entry point for the per-present JSONL trace -- called once per actual
// presentation (real or concealed) from the shared stats block in
// vk_render_thread, vk_video_impl_windows.c. concealed=1 for a synthesized
// frame, 0 for a real one; t is extrapolateT (meaningless, passed as -1,
// for a real frame). Never blocks the render thread on disk I/O beyond
// normal buffered-writer cost -- os.File writes here are NOT fsync'd per
// line, same tradeoff as logrus's own async writer elsewhere in this app.
//
//export goFrameSmoothingTraceWrite
func goFrameSmoothingTraceWrite(concealed C.int, t, centerVx, centerVy, wholeVx, wholeVy, nonzeroPct, gapMs C.double) {
	frameSmoothingTraceMu.Lock()
	defer frameSmoothingTraceMu.Unlock()
	if !frameSmoothingTraceEnsureOpen() {
		return
	}
	kind := "real"
	if concealed != 0 {
		kind = "concealed"
	}
	now := time.Now()
	_ = frameSmoothingTraceEnc.Encode(frameSmoothingTraceEntry{
		TsUnixMs:   now.UnixMilli(),
		Ts:         now.Format("2006-01-02T15:04:05.000Z07:00"),
		Kind:       kind,
		T:          float64(t),
		CenterVx:   float64(centerVx),
		CenterVy:   float64(centerVy),
		WholeVx:    float64(wholeVx),
		WholeVy:    float64(wholeVy),
		NonzeroPct: float64(nonzeroPct),
		GapMs:      float64(gapMs),
	})
}
