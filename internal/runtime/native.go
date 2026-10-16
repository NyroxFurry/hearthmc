package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Native runs processes directly on the host.
type Native struct{}

func (Native) Name() string                    { return "native" }
func (Native) Check(ctx context.Context) error { return nil }

func (Native) Start(ctx context.Context, spec *Spec) (Process, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("empty command")
	}
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	setProcAttrs(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, fmt.Errorf("start %s: %w", spec.Argv[0], err)
	}
	// The child owns its copy of the write end now; close ours so the reader
	// sees EOF once the child (and all its children) exit.
	pw.Close()

	p := &nativeProc{cmd: cmd, stdin: stdin, out: pr, done: make(chan struct{})}
	if proc, err := process.NewProcess(int32(cmd.Process.Pid)); err == nil {
		p.ps = proc
	}
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
				if code < 0 { // killed by signal
					code = 137
				}
			} else {
				code = -1
			}
		}
		p.exitCode = code
		close(p.done)
		// give the reader a moment to drain buffered output before closing
		time.AfterFunc(2*time.Second, func() { pr.Close() })
	}()
	return p, nil
}

type nativeProc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	out      *os.File
	done     chan struct{}
	exitCode int
	ps       *process.Process

	mu       sync.Mutex
	lastCPU  float64
	lastTime time.Time
}

func (p *nativeProc) ID() string        { return strconv.Itoa(p.cmd.Process.Pid) }
func (p *nativeProc) HostPID() int      { return p.cmd.Process.Pid }
func (p *nativeProc) Output() io.Reader { return p.out }

func (p *nativeProc) Write(b []byte) (int, error) {
	select {
	case <-p.done:
		return 0, ErrNotRunning
	default:
	}
	return p.stdin.Write(b)
}

func (p *nativeProc) Wait() (int, error) {
	<-p.done
	return p.exitCode, nil
}

func (p *nativeProc) Terminate() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	return terminateTree(p.cmd)
}

func (p *nativeProc) Kill() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	return killTree(p.cmd)
}

// Usage sums CPU and memory over the process and its children.
func (p *nativeProc) Usage() (Usage, error) {
	select {
	case <-p.done:
		return Usage{}, ErrNotRunning
	default:
	}
	if p.ps == nil {
		return Usage{}, errors.New("process stats unavailable")
	}
	procs := []*process.Process{p.ps}
	collectChildren(p.ps, &procs, 0)

	var cpuTotal float64
	var rss uint64
	for _, pr := range procs {
		if t, err := pr.Times(); err == nil {
			cpuTotal += t.User + t.System
		}
		if m, err := pr.MemoryInfo(); err == nil {
			rss += m.RSS
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	var pct float64
	if !p.lastTime.IsZero() {
		dt := now.Sub(p.lastTime).Seconds()
		if dt > 0 && cpuTotal >= p.lastCPU {
			pct = (cpuTotal - p.lastCPU) / dt * 100
		}
	}
	p.lastCPU, p.lastTime = cpuTotal, now
	return Usage{CPU: pct, MemBytes: rss}, nil
}

func collectChildren(p *process.Process, out *[]*process.Process, depth int) {
	if depth > 4 {
		return
	}
	kids, err := p.Children()
	if err != nil {
		return
	}
	for _, k := range kids {
		*out = append(*out, k)
		collectChildren(k, out, depth+1)
	}
}
