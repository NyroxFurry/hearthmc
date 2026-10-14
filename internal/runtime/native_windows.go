//go:build windows

package runtime

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup, HideWindow: true}
}

// Windows has no SIGTERM for console processes we do not share a console
// with; game servers are stopped via their stop command first, so a hard
// kill of the tree is the fallback.
func terminateTree(cmd *exec.Cmd) error { return killTree(cmd) }

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
