//go:build linux

package main

import "syscall"

// dupOnto makes newfd refer to oldfd's file, closing whatever newfd referred to
// before, as one atomic step.
//
// Dup3 rather than Dup2: linux/arm64 has no dup2 syscall, and .goreleaser.yaml
// builds linux/arm64.
func dupOnto(oldfd, newfd int) error { return syscall.Dup3(oldfd, newfd, 0) }
