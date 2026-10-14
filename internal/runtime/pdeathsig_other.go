//go:build !linux && !windows

package runtime

import "syscall"

func setPdeathsig(a *syscall.SysProcAttr) {}
