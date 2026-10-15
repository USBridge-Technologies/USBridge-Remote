// ndkclangwrap stands in for an NDK clang shell script on Windows.
// cmd.exe refuses the link line once it passes 8191 characters. This
// program is copied over that script and hands clang.exe a response file.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	self := filepath.Base(os.Args[0])
	self = strings.TrimSuffix(strings.ToLower(self), ".exe")
	tool := "clang.exe"
	target := self
	switch {
	case strings.HasSuffix(self, "-clang++"):
		tool = "clang++.exe"
		target = strings.TrimSuffix(self, "-clang++")
	case strings.HasSuffix(self, "-clang"):
		target = strings.TrimSuffix(self, "-clang")
	}
	args := os.Args[1:]
	if len(args) == 0 || args[0] != "-cc1" {
		args = append([]string{"--target=" + target}, args...)
	}
	rsp, err := os.CreateTemp("", "ndkcc-*.rsp")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rspName := rsp.Name()
	defer os.Remove(rspName)
	for _, arg := range args {
		fmt.Fprintln(rsp, quoteRsp(arg))
	}
	if err := rsp.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cmd := exec.Command(filepath.Join(filepath.Dir(os.Args[0]), tool), "@"+filepath.ToSlash(rspName))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func quoteRsp(s string) string {
	s = filepath.ToSlash(s)
	if s == "" || strings.ContainsAny(s, " \t\n\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
