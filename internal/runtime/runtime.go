// Package runtime abstracts how a game server process is executed:
// directly on the host ("native") or inside a Docker container ("docker").
package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
)

// Spec describes a process to start.
type Spec struct {
	ServerID string
	Dir      string   // host directory containing the server files
	Argv     []string // program and arguments (for docker: as seen inside the container)
	Env      []string // extra environment variables (KEY=VALUE)
	MemoryMB int      // docker: hard memory limit in MiB (0 = unlimited)
	CPUs     float64  // docker: CPU limit in cores (0 = unlimited)
	Ports    []Port   // docker: published ports
	Image    string   // docker: image to run
	Labels   map[string]string
}

type Port struct {
	Host      int
	Container int
	Proto     string // tcp or udp
	HostIP    string // bind address on the host, "" = all interfaces
}

// Usage is a resource usage sample.
type Usage struct {
	CPU      float64 `json:"cpu"`      // percent of one core (200 = two full cores)
	MemBytes uint64  `json:"mem"`      // resident memory
	MemLimit uint64  `json:"memLimit"` // 0 = unknown/unlimited
}

// Process is a running game server process.
type Process interface {
	ID() string
	// Output returns the combined stdout/stderr stream. It returns io.EOF once the process has exited.
	Output() io.Reader
	// Write sends bytes to the process' stdin.
	Write(p []byte) (int, error)
	// Wait blocks until the process exits and returns its exit code.
	Wait() (int, error)
	// Terminate asks the process to exit gracefully (SIGTERM).
	Terminate() error
	// Kill forcefully stops the process (and its children).
	Kill() error
	Usage() (Usage, error)
}

// HostPIDer is implemented by processes that can name the PID of their main
// process on this host (used to read the Java heap). 0 means unknown.
type HostPIDer interface {
	HostPID() int
}

type Runtime interface {
	Name() string
	// Check reports whether this runtime is usable on this host.
	Check(ctx context.Context) error
	Start(ctx context.Context, spec *Spec) (Process, error)
}

// Reattacher is implemented by runtimes whose processes can survive a panel restart.
type Reattacher interface {
	// Reattach returns the still running process for serverID, or (nil, nil) if there is none.
	Reattach(ctx context.Context, serverID string) (Process, []string, error)
}

var ErrNotRunning = errors.New("process not running")

// HostTimezone returns the IANA name of the host's time zone (best effort),
// so containers log with the same local time as the host.
func HostTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return strings.TrimPrefix(tz, ":")
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			return target[i+len("zoneinfo/"):]
		}
	}
	return ""
}
