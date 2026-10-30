package main

import (
	"strconv"
	"strings"
)

// glVersionAtLeast parses GL_VERSION ("4.6.0 NVIDIA 560.94", "1.1.0").
func glVersionAtLeast(version string, major, minor int) bool {
	words := strings.Fields(version)
	if len(words) == 0 {
		return false
	}
	fields := strings.SplitN(words[0], ".", 3)
	if len(fields) < 2 {
		return false
	}
	gotMajor, err1 := strconv.Atoi(fields[0])
	gotMinor, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}
