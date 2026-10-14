package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Docker runs game servers in containers via the Docker Engine HTTP API.
// It talks to the daemon directly (no docker CLI or SDK needed).
type Docker struct {
	Host string // unix:///var/run/docker.sock or tcp://host:2375
	// OnPull receives image pull progress messages (optional).
	OnPull func(serverID, msg string)

	once    sync.Once
	network string
	addr    string
	client  *http.Client
	verMu   sync.Mutex
	apiVer  string
}

func NewDocker(host string) *Docker {
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	return &Docker{Host: host}
}

func (d *Docker) Name() string { return "docker" }

func (d *Docker) init() {
	d.once.Do(func() {
		switch {
		case strings.HasPrefix(d.Host, "unix://"):
			d.network, d.addr = "unix", strings.TrimPrefix(d.Host, "unix://")
		case strings.HasPrefix(d.Host, "tcp://"):
			d.network, d.addr = "tcp", strings.TrimPrefix(d.Host, "tcp://")
		default:
			d.network, d.addr = "unix", d.Host
		}
		tr := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dl net.Dialer
				return dl.DialContext(ctx, d.network, d.addr)
			},
			MaxIdleConns:    10,
			IdleConnTimeout: 30 * time.Second,
		}
		d.client = &http.Client{Transport: tr}
	})
}

// version negotiates the API version with the daemon (Docker 29+ rejects old ones).
func (d *Docker) version(ctx context.Context) (string, error) {
	d.init()
	d.verMu.Lock()
	defer d.verMu.Unlock()
	if d.apiVer != "" {
		return d.apiVer, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://docker/_ping", nil)
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	v := resp.Header.Get("API-Version")
	if v == "" {
		v = "1.41"
	}
	d.apiVer = v
	return v, nil
}

func (d *Docker) do(ctx context.Context, method, path string, query url.Values, body any, out any) (*http.Response, error) {
	ver, err := d.version(ctx)
	if err != nil {
		return nil, fmt.Errorf("Docker nicht erreichbar (%s): %w", d.Host, err)
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := "http://docker/v" + ver + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var e struct {
			Message string `json:"message"`
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		json.Unmarshal(b, &e)
		if e.Message == "" {
			e.Message = strings.TrimSpace(string(b))
		}
		return resp, &DockerError{Status: resp.StatusCode, Message: e.Message}
	}
	if out != nil {
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

type DockerError struct {
	Status  int
	Message string
}

func (e *DockerError) Error() string { return fmt.Sprintf("docker: %s (HTTP %d)", e.Message, e.Status) }

func isNotFound(err error) bool {
	var de *DockerError
	return errors.As(err, &de) && de.Status == 404
}

func (d *Docker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := d.version(ctx)
	if err != nil {
		return fmt.Errorf("Docker nicht erreichbar unter %s: %w", d.Host, err)
	}
	return nil
}

// Info returns basic daemon information for the admin page.
func (d *Docker) Info(ctx context.Context) (map[string]any, error) {
	var info map[string]any
	_, err := d.do(ctx, "GET", "/info", nil, nil, &info)
	return info, err
}

func containerName(serverID string) string { return "hearth-" + serverID }

// EnsureImage pulls the image if it is not present locally.
func (d *Docker) EnsureImage(ctx context.Context, serverID, image string) error {
	resp, err := d.do(ctx, "GET", "/images/"+url.PathEscape(image)+"/json", nil, nil, nil)
	if err == nil {
		resp.Body.Close()
		return nil
	}
	if !isNotFound(err) {
		return err
	}
	name, tag := image, "latest"
	if i := strings.LastIndex(image, ":"); i > 0 && !strings.Contains(image[i:], "/") {
		name, tag = image[:i], image[i+1:]
	}
	q := url.Values{"fromImage": {name}, "tag": {tag}}
	resp, err = d.do(ctx, "POST", "/images/create", q, nil, nil)
	if err != nil {
		return fmt.Errorf("Image %s konnte nicht geladen werden: %w", image, err)
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	lastReport := time.Time{}
	for {
		var msg struct {
			Status   string `json:"status"`
			Progress string `json:"progress"`
			ID       string `json:"id"`
			Error    string `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if msg.Error != "" {
			return fmt.Errorf("Image %s: %s", image, msg.Error)
		}
		if d.OnPull != nil && time.Since(lastReport) > 700*time.Millisecond {
			lastReport = time.Now()
			d.OnPull(serverID, strings.TrimSpace(msg.ID+" "+msg.Status+" "+msg.Progress))
		}
	}
	if d.OnPull != nil {
		d.OnPull(serverID, "Image "+image+" bereit")
	}
	return nil
}

func (d *Docker) removeContainer(ctx context.Context, idOrName string) {
	resp, err := d.do(ctx, "DELETE", "/containers/"+url.PathEscape(idOrName), url.Values{"force": {"1"}, "v": {"1"}}, nil, nil)
	if err == nil {
		resp.Body.Close()
	}
}

func (d *Docker) Start(ctx context.Context, spec *Spec) (Process, error) {
	if spec.Image == "" {
		return nil, errors.New("kein Docker-Image angegeben")
	}
	if err := d.EnsureImage(ctx, spec.ServerID, spec.Image); err != nil {
		return nil, err
	}
	name := containerName(spec.ServerID)
	d.removeContainer(ctx, name) // leftovers from a crash

	exposed := map[string]struct{}{}
	bindings := map[string][]map[string]string{}
	for _, p := range spec.Ports {
		proto := p.Proto
		if proto == "" {
			proto = "tcp"
		}
		key := fmt.Sprintf("%d/%s", p.Container, proto)
		exposed[key] = struct{}{}
		bindings[key] = append(bindings[key], map[string]string{"HostIp": p.HostIP, "HostPort": strconv.Itoa(p.Host)})
	}
	labels := map[string]string{"hearth.managed": "true", "hearth.server": spec.ServerID}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	host := map[string]any{
		"Binds":         []string{spec.Dir + ":/data"},
		"PortBindings":  bindings,
		"RestartPolicy": map[string]any{"Name": "no"},
		"Init":          true,
		"SecurityOpt":   []string{"no-new-privileges"},
		"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "20m", "max-file": "2"}},
	}
	if spec.MemoryMB > 0 {
		host["Memory"] = int64(spec.MemoryMB) << 20
		host["MemorySwap"] = int64(spec.MemoryMB) << 20
	}
	if spec.CPUs > 0 {
		host["NanoCpus"] = int64(spec.CPUs * 1e9)
	}
	env := []string{"HOME=/tmp"}
	if tz := HostTimezone(); tz != "" {
		env = append(env, "TZ="+tz)
	}
	env = append(env, spec.Env...)
	body := map[string]any{
		"Image":        spec.Image,
		"Cmd":          spec.Argv,
		"WorkingDir":   "/data",
		"Env":          env,
		"Labels":       labels,
		"OpenStdin":    true,
		"StdinOnce":    false,
		"Tty":          false,
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"ExposedPorts": exposed,
		"HostConfig":   host,
	}
	if uid := os.Getuid(); uid >= 0 {
		body["User"] = fmt.Sprintf("%d:%d", uid, os.Getgid())
	}
	var created struct {
		ID string `json:"Id"`
	}
	if _, err := d.do(ctx, "POST", "/containers/create", url.Values{"name": {name}}, body, &created); err != nil {
		return nil, err
	}
	p, err := d.attach(ctx, created.ID)
	if err != nil {
		d.removeContainer(ctx, created.ID)
		return nil, err
	}
	resp, err := d.do(ctx, "POST", "/containers/"+created.ID+"/start", nil, nil, nil)
	if err != nil {
		p.conn.Close()
		d.removeContainer(ctx, created.ID)
		return nil, err
	}
	resp.Body.Close()
	p.watch()
	return p, nil
}

// Reattach finds a running container for the server after a panel restart.
func (d *Docker) Reattach(ctx context.Context, serverID string) (Process, []string, error) {
	var list []struct {
		ID    string `json:"Id"`
		State string `json:"State"`
	}
	filters, _ := json.Marshal(map[string][]string{"label": {"hearth.server=" + serverID}})
	if _, err := d.do(ctx, "GET", "/containers/json", url.Values{"all": {"1"}, "filters": {string(filters)}}, nil, &list); err != nil {
		return nil, nil, err
	}
	for _, c := range list {
		if c.State != "running" {
			d.removeContainer(ctx, c.ID)
			continue
		}
		history := d.tailLogs(ctx, c.ID, 300)
		p, err := d.attach(ctx, c.ID)
		if err != nil {
			return nil, nil, err
		}
		p.watch()
		return p, history, nil
	}
	return nil, nil, nil
}

func (d *Docker) tailLogs(ctx context.Context, id string, n int) []string {
	resp, err := d.do(ctx, "GET", "/containers/"+id+"/logs", url.Values{"stdout": {"1"}, "stderr": {"1"}, "tail": {strconv.Itoa(n)}}, nil, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(demux(resp.Body, pw)) }()
	var lines []string
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func (d *Docker) attach(ctx context.Context, id string) (*dockerProc, error) {
	ver, err := d.version(ctx)
	if err != nil {
		return nil, err
	}
	var dl net.Dialer
	conn, err := dl.DialContext(ctx, d.network, d.addr)
	if err != nil {
		return nil, err
	}
	path := "/v" + ver + "/containers/" + id + "/attach?stream=1&stdin=1&stdout=1&stderr=1"
	req := "POST " + path + " HTTP/1.1\r\nHost: docker\r\nUpgrade: tcp\r\nConnection: Upgrade\r\nContent-Length: 0\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	hreq, _ := http.NewRequest("POST", path, nil)
	resp, err := http.ReadResponse(br, hreq)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		conn.Close()
		return nil, fmt.Errorf("docker attach: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	pr, pw := io.Pipe()
	p := &dockerProc{d: d, id: id, conn: conn, out: pr, done: make(chan struct{})}
	go func() {
		err := demux(br, pw)
		pw.CloseWithError(err)
	}()
	return p, nil
}

// demux splits Docker's multiplexed stream (8 byte header per frame) into w.
func demux(r io.Reader, w io.Writer) error {
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if err == io.ErrUnexpectedEOF {
				return io.EOF
			}
			return err
		}
		n := binary.BigEndian.Uint32(hdr[4:])
		if _, err := io.CopyN(w, r, int64(n)); err != nil {
			return err
		}
	}
}

type dockerProc struct {
	d        *Docker
	id       string
	conn     net.Conn
	out      *io.PipeReader
	done     chan struct{}
	exitCode int

	mu        sync.Mutex
	lastTotal uint64
	lastSys   uint64
}

func (p *dockerProc) ID() string        { return p.id[:min(12, len(p.id))] }
func (p *dockerProc) Output() io.Reader { return p.out }

func (p *dockerProc) Write(b []byte) (int, error) {
	select {
	case <-p.done:
		return 0, ErrNotRunning
	default:
	}
	return p.conn.Write(b)
}

// watch waits for the container to exit in the background.
func (p *dockerProc) watch() {
	go func() {
		var res struct {
			StatusCode int `json:"StatusCode"`
		}
		for {
			_, err := p.d.do(context.Background(), "POST", "/containers/"+p.id+"/wait", url.Values{"condition": {"not-running"}}, nil, &res)
			if err == nil || isNotFound(err) {
				break
			}
			time.Sleep(2 * time.Second) // daemon restart etc.
		}
		p.exitCode = res.StatusCode
		close(p.done)
		time.AfterFunc(2*time.Second, func() { p.conn.Close() })
		p.d.removeContainer(context.Background(), p.id)
	}()
}

func (p *dockerProc) Wait() (int, error) {
	<-p.done
	return p.exitCode, nil
}

func (p *dockerProc) signal(sig string) error {
	select {
	case <-p.done:
		return nil
	default:
	}
	resp, err := p.d.do(context.Background(), "POST", "/containers/"+p.id+"/kill", url.Values{"signal": {sig}}, nil, nil)
	if err != nil {
		var de *DockerError
		if errors.As(err, &de) && (de.Status == 404 || de.Status == 409) {
			return nil
		}
		return err
	}
	resp.Body.Close()
	return nil
}

func (p *dockerProc) Terminate() error { return p.signal("SIGTERM") }
func (p *dockerProc) Kill() error      { return p.signal("SIGKILL") }

func (p *dockerProc) Usage() (Usage, error) {
	select {
	case <-p.done:
		return Usage{}, ErrNotRunning
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var st struct {
		CPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
			Online int    `json:"online_cpus"`
		} `json:"cpu_stats"`
		Mem struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if _, err := p.d.do(ctx, "GET", "/containers/"+p.id+"/stats", url.Values{"stream": {"0"}, "one-shot": {"1"}}, nil, &st); err != nil {
		return Usage{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var pct float64
	if p.lastSys > 0 && st.CPU.System > p.lastSys && st.CPU.Usage.Total >= p.lastTotal {
		online := st.CPU.Online
		if online == 0 {
			online = 1
		}
		pct = float64(st.CPU.Usage.Total-p.lastTotal) / float64(st.CPU.System-p.lastSys) * float64(online) * 100
	}
	p.lastTotal, p.lastSys = st.CPU.Usage.Total, st.CPU.System
	mem := st.Mem.Usage
	if v, ok := st.Mem.Stats["inactive_file"]; ok && v < mem {
		mem -= v
	} else if v, ok := st.Mem.Stats["total_inactive_file"]; ok && v < mem {
		mem -= v
	}
	return Usage{CPU: pct, MemBytes: mem, MemLimit: st.Mem.Limit}, nil
}
