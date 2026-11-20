//go:build !windows

package main

import "log"

func showError(msg string) { log.Print(msg) }
