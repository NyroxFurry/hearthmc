package jvm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// socketPath is where a JVM's attach listener listens, seen from the host:
// /tmp inside the process' own mount namespace (a container or a systemd
// PrivateTmp), under the PID the process has in its own PID namespace.
func socketPath(pid int) string {
	return fmt.Sprintf("/proc/%d/root/tmp/.java_pid%d", pid, nsPID(pid))
}

// nsPID returns the PID of a process inside its innermost PID namespace.
func nsPID(pid int) int {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return pid
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "NSpid:"); ok {
			fields := strings.Fields(v)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
					return n
				}
			}
			break
		}
	}
	return pid
}

func hasListener(pid int) bool {
	st, err := os.Stat(socketPath(pid))
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// FindJava returns the first process in the tree below (and including) pid
// that runs a JVM with an attach listener, or 0.
func FindJava(pid int) int {
	queue := []int{pid}
	for depth := 0; depth < 5 && len(queue) > 0; depth++ {
		var next []int
		for _, p := range queue {
			if hasListener(p) {
				return p
			}
			next = append(next, children(p)...)
		}
		queue = next
	}
	return 0
}

// children lists the child processes of pid.
func children(pid int) []int {
	var out []int
	tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return nil
	}
	supported := false
	for _, t := range tasks {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", pid, t.Name()))
		if err != nil {
			continue
		}
		supported = true
		for _, f := range strings.Fields(string(b)) {
			if n, err := strconv.Atoi(f); err == nil {
				out = append(out, n)
			}
		}
	}
	if supported {
		return out
	}
	// kernels without CONFIG_PROC_CHILDREN: scan all processes for the parent PID
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// "pid (comm) state ppid ...": comm may contain spaces, so cut at the last ')'
		s := string(b)
		if i := strings.LastIndexByte(s, ')'); i > 0 {
			f := strings.Fields(s[i+1:])
			if len(f) > 1 && f[1] == strconv.Itoa(pid) {
				out = append(out, n)
			}
		}
	}
	return out
}

// Command runs a diagnostic command (like jcmd) in the JVM with the given
// host PID and returns its output.
func Command(ctx context.Context, pid int, cmd string) (string, error) {
	path := socketPath(pid)
	if st, err := os.Stat(path); err != nil || st.Mode()&os.ModeSocket == 0 {
		return "", ErrNoListener
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)
	if _, err := conn.Write(request("jcmd", cmd)); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(conn, 1<<20))
	if err != nil && len(b) == 0 {
		return "", err
	}
	code, out, err := parseReply(b)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("jcmd %s: %s", cmd, strings.TrimSpace(out))
	}
	return out, nil
}

// HeapInfo reads the heap usage of the JVM with the given host PID. It needs
// Java 9 or newer (GC.heap_info); Java 8 reports an error and callers fall
// back to the resident memory of the process.
func HeapInfo(ctx context.Context, pid int) (Heap, error) {
	out, err := Command(ctx, pid, "GC.heap_info")
	if err != nil {
		return Heap{}, err
	}
	if h, ok := parseHeapInfo(out); ok {
		return h, nil
	}
	return Heap{}, fmt.Errorf("unknown GC.heap_info format")
}
