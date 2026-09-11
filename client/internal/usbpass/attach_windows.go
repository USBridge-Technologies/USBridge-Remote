//go:build !unix

package usbpass

import (
	"os"
	"os/exec"
)

func setAttachProcAttr(cmd *exec.Cmd) {}

func killAttachProcess(p *os.Process) {
	if p != nil {
		_ = p.Kill()
	}
}

func killOrphanClientBrokers() {}
