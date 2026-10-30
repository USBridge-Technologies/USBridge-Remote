//go:build !windows

package main

const glProbeArg = "gl-probe"

func useSoftwareGLIfNeeded() {}

func runGLProbe() int { return 0 }
