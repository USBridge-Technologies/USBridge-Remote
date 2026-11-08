//go:build !linux || android

package main

import "fmt"

func startRenderer(kind string, w, h int, vsync bool) (func(), func() string, error) {
	return nil, nil, fmt.Errorf("-render is Linux only")
}

func runOnWayland(kind string, w, h int, vsync bool, body func(func() string)) { body(nil) }
func setPhase(reporting bool, arm int)                                        {}
func dumpPhase(path string) int                                               { return 0 }
