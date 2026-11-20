//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func configureHelperProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008}
	return nil
}
