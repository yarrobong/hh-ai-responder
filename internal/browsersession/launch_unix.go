//go:build !windows

package browsersession

import (
	"os/exec"
	"syscall"
)

func startDetachedBrowser(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
