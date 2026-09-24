//go:build !linux

package streamhost

func killOrphansByArgv0(names []string) {}
