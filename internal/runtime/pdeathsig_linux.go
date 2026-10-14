//go:build linux

package runtime

import "syscall"

// If the panel dies unexpectedly, the game server receives SIGTERM and can
// shut down cleanly instead of being orphaned while holding its ports.
func setPdeathsig(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGTERM }
