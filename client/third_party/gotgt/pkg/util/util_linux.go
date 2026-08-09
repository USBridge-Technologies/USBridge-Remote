//go:build linux
// +build linux

/*
Copyright 2016 The GoStor Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"os"
	"syscall"
)

func Fdatasync(file *os.File) error {
	return syscall.Fdatasync(int(file.Fd()))
}

// Fadvise is a readahead-pattern hint to the kernel (posix_fadvise);
// moved here (Linux-only, matches the build-tagged file it now lives in)
// from the shared util.go, where it broke every non-Linux build — it used
// syscall.Syscall6 with a Linux-specific SYS_FADVISE64 raw syscall number,
// unconditionally, which doesn't even type-check against the Windows
// syscall package's different Syscall6 signature.
func Fadvise(file *os.File, off, length int64, advise uint32) error {
	// syscall.SYS_FADVISE64 = 221
	_, _, err := syscall.Syscall6(221, file.Fd(), uintptr(off), uintptr(length), uintptr(advise), 0, 0)
	if err != 0 {
		return err
	}
	return nil
}
