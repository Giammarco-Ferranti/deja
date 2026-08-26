//go:build darwin

package main

import "syscall"

// dupOnto makes newfd refer to oldfd's file, closing whatever newfd referred to
// before. Darwin has no dup3; dup2 is the equivalent here.
func dupOnto(oldfd, newfd int) error { return syscall.Dup2(oldfd, newfd) }
