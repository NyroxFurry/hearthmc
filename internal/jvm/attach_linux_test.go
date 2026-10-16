package jvm

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealJVM starts a small Java program the way Hearth starts servers and
// reads its heap. Skipped when no JDK is installed.
func TestRealJVM(t *testing.T) {
	java, err := exec.LookPath("java")
	if err != nil {
		t.Skip("java not installed")
	}
	dir := t.TempDir()
	src := `public class Keep {
  public static void main(String[] a) throws Exception {
    byte[][] keep = new byte[24][];
    for (int i = 0; i < keep.length; i++) keep[i] = new byte[1 << 20];
    System.out.println("ready " + keep.length);
    Thread.sleep(120000);
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "Keep.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(java, "-Xms64m", "-Xmx128m", "-XX:+UseG1GC", "-XX:+StartAttachListener", "-XX:+PerfDisableSharedMem", "Keep.java")
	cmd.Dir = dir
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	ready := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "ready") {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("java exited early")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("java did not start")
	}
	var pid int
	for i := 0; i < 50 && pid == 0; i++ {
		pid = FindJava(cmd.Process.Pid)
		if pid == 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if pid != cmd.Process.Pid {
		t.Fatalf("FindJava = %d, want %d", pid, cmd.Process.Pid)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h, err := HeapInfo(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("heap: used %d MiB, committed %d MiB", h.Used>>20, h.Committed>>20)
	if h.Used < 24<<20 || h.Committed < h.Used || h.Committed > 128<<20 {
		t.Fatalf("implausible heap %+v", h)
	}
	if _, err := Command(ctx, pid, "No.such_command"); err == nil {
		t.Fatal("expected an error for an unknown command")
	}
}
