//go:build !windows

package main

import "os/exec"

func configureHelperProcess(cmd *exec.Cmd) error { return nil }
