//go:build windows

package browsersession

import (
	"os/exec"
	"syscall"
)

func startDetachedBrowser(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
	return cmd.Start()
}
