//go:build !linux

package jvm

import "context"

// FindJava is only implemented on Linux.
func FindJava(pid int) int { return 0 }

// Command is only implemented on Linux.
func Command(ctx context.Context, pid int, cmd string) (string, error) { return "", ErrUnsupported }

// HeapInfo is only implemented on Linux.
func HeapInfo(ctx context.Context, pid int) (Heap, error) { return Heap{}, ErrUnsupported }
