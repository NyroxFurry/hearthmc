package mapr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DimSpec describes a dimension to render.
type DimSpec struct {
	Key       string // overworld, the_nether, ...
	RegionDir string
	Nether    bool
}

type Progress struct {
	Running    bool   `json:"running"`
	Dim        string `json:"dim"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	Rendered   int    `json:"rendered"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	Error      string `json:"error,omitempty"`
}

// DimInfo describes the rendered tiles of a dimension.
type DimInfo struct {
	Key        string `json:"key"`
	MinZoom    int    `json:"minZoom"`
	MinX       int    `json:"minX"` // region coordinates of the rendered area
	MinZ       int    `json:"minZ"`
	MaxX       int    `json:"maxX"`
	MaxZ       int    `json:"maxZ"`
	Regions    int    `json:"regions"`
	LastRender int64  `json:"lastRender"`
}

type dimState struct {
	Regions    map[string]int64 `json:"regions"` // "x,z" -> region file mtime (ns)
	MinZoom    int              `json:"minZoom"`
	LastRender int64            `json:"lastRender"`
}

type Service struct {
	Dir        string
	Threads    func() int
	OnProgress func(serverID string, p Progress)
	Log        *slog.Logger

	mu   sync.Mutex
	jobs map[string]*Progress
}

const maxZoomOut = 8

func (s *Service) dimDir(serverID, dim string) string {
	return filepath.Join(s.Dir, serverID, dim)
}

// TilePath returns the file for a tile (may not exist).
func (s *Service) TilePath(serverID, dim string, z, x, y int) string {
	return filepath.Join(s.dimDir(serverID, dim), strconv.Itoa(z), fmt.Sprintf("%d_%d.png", x, y))
}

func (s *Service) loadState(serverID, dim string) *dimState {
	st := &dimState{Regions: map[string]int64{}}
	b, err := os.ReadFile(filepath.Join(s.dimDir(serverID, dim), "state.json"))
	if err == nil {
		json.Unmarshal(b, st)
	}
	if st.Regions == nil {
		st.Regions = map[string]int64{}
	}
	return st
}

func (s *Service) saveState(serverID, dim string, st *dimState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	dir := s.dimDir(serverID, dim)
	os.MkdirAll(dir, 0o755)
	tmp := filepath.Join(dir, "state.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "state.json"))
}

// Info returns the rendered state of a dimension.
func (s *Service) Info(serverID, dim string) DimInfo {
	st := s.loadState(serverID, dim)
	info := DimInfo{Key: dim, MinZoom: st.MinZoom, LastRender: st.LastRender, Regions: len(st.Regions)}
	first := true
	for k := range st.Regions {
		x, z, ok := parseKey(k)
		if !ok {
			continue
		}
		if first {
			info.MinX, info.MaxX, info.MinZ, info.MaxZ = x, x, z, z
			first = false
		}
		info.MinX, info.MaxX = min(info.MinX, x), max(info.MaxX, x)
		info.MinZ, info.MaxZ = min(info.MinZ, z), max(info.MaxZ, z)
	}
	return info
}

func parseKey(k string) (int, int, bool) {
	a, b, ok := strings.Cut(k, ",")
	if !ok {
		return 0, 0, false
	}
	x, e1 := strconv.Atoi(a)
	z, e2 := strconv.Atoi(b)
	return x, z, e1 == nil && e2 == nil
}

func (s *Service) Status(serverID string) Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.jobs[serverID]; ok {
		return *p
	}
	return Progress{}
}

func (s *Service) report(serverID string, p *Progress) {
	s.mu.Lock()
	cp := *p
	s.mu.Unlock()
	if s.OnProgress != nil {
		s.OnProgress(serverID, cp)
	}
}

var ErrRunning = errors.New("Für diesen Server läuft bereits ein Render-Vorgang")

// Render starts rendering the given dimensions in the background.
// full=true re-renders all regions instead of only changed ones.
func (s *Service) Render(serverID string, dims []DimSpec, full bool) error {
	s.mu.Lock()
	if s.jobs == nil {
		s.jobs = map[string]*Progress{}
	}
	if p, ok := s.jobs[serverID]; ok && p.Running {
		s.mu.Unlock()
		return ErrRunning
	}
	p := &Progress{Running: true, StartedAt: time.Now().Unix()}
	s.jobs[serverID] = p
	s.mu.Unlock()
	go func() {
		var errs []string
		for _, d := range dims {
			if err := s.renderDim(serverID, d, full, p); err != nil {
				errs = append(errs, d.Key+": "+err.Error())
			}
		}
		s.mu.Lock()
		p.Running = false
		p.FinishedAt = time.Now().Unix()
		p.Error = strings.Join(errs, "; ")
		s.mu.Unlock()
		s.report(serverID, p)
	}()
	return nil
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func (s *Service) renderDim(serverID string, d DimSpec, full bool, p *Progress) error {
	entries, err := os.ReadDir(d.RegionDir)
	if err != nil {
		return err
	}
	st := s.loadState(serverID, d.Key)
	type job struct {
		x, z  int
		path  string
		mtime int64
	}
	var todo []job
	present := map[string]bool{}
	for _, e := range entries {
		x, z, ok := ParseRegionName(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() < 8192 {
			continue
		}
		key := fmt.Sprintf("%d,%d", x, z)
		present[key] = true
		mt := info.ModTime().UnixNano()
		if full || st.Regions[key] != mt {
			todo = append(todo, job{x, z, filepath.Join(d.RegionDir, e.Name()), mt})
		}
	}
	dirty := map[[2]int]bool{}
	// regions that vanished (world reset): remove their tiles
	for key := range st.Regions {
		if !present[key] {
			x, z, _ := parseKey(key)
			os.Remove(s.TilePath(serverID, d.Key, 0, x, z))
			delete(st.Regions, key)
			dirty[[2]int{x, z}] = true
		}
	}
	s.mu.Lock()
	p.Dim, p.Done, p.Total = d.Key, 0, len(todo)
	s.mu.Unlock()
	s.report(serverID, p)

	threads := 2
	if s.Threads != nil {
		threads = max(1, s.Threads())
	}
	opt := RenderOptions{}
	if d.Nether {
		opt.Ceiling = 110
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ch := make(chan job)
	lastReport := time.Now()
	for range threads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				img, chunks, err := RenderRegion(j.path, opt)
				mu.Lock()
				if err == nil {
					if chunks > 0 {
						if werr := writePNG(s.TilePath(serverID, d.Key, 0, j.x, j.z), img); werr == nil {
							st.Regions[fmt.Sprintf("%d,%d", j.x, j.z)] = j.mtime
						}
					} else {
						os.Remove(s.TilePath(serverID, d.Key, 0, j.x, j.z))
						delete(st.Regions, fmt.Sprintf("%d,%d", j.x, j.z))
					}
					dirty[[2]int{j.x, j.z}] = true
				} else if s.Log != nil {
					s.Log.Warn("render region", "path", j.path, "err", err)
				}
				s.mu.Lock()
				p.Done++
				if chunks > 0 {
					p.Rendered++
				}
				s.mu.Unlock()
				report := time.Since(lastReport) > time.Second
				if report {
					lastReport = time.Now()
				}
				mu.Unlock()
				if report {
					s.report(serverID, p)
				}
			}
		}()
	}
	for _, j := range todo {
		ch <- j
	}
	close(ch)
	wg.Wait()

	// zoom levels: each level halves the resolution
	minX, minZ, maxX, maxZ := math.MaxInt32, math.MaxInt32, math.MinInt32, math.MinInt32
	for key := range st.Regions {
		x, z, _ := parseKey(key)
		minX, minZ, maxX, maxZ = min(minX, x), min(minZ, z), max(maxX, x), max(maxZ, z)
	}
	minZoom := 0
	if len(st.Regions) > 0 {
		span := max(maxX-minX, maxZ-minZ) + 1
		for span > 1 && minZoom > -maxZoomOut {
			span = (span + 1) / 2
			minZoom--
		}
		minZoom = max(minZoom-1, -maxZoomOut)
	}
	for z := -1; z >= minZoom; z-- {
		next := map[[2]int]bool{}
		for t := range dirty {
			next[[2]int{floorDiv(t[0], 2), floorDiv(t[1], 2)}] = true
		}
		for t := range next {
			s.composeParent(serverID, d.Key, z, t[0], t[1])
		}
		dirty = next
	}
	// remove zoom levels that are no longer needed
	for z := minZoom - 1; z >= -maxZoomOut; z-- {
		os.RemoveAll(filepath.Join(s.dimDir(serverID, d.Key), strconv.Itoa(z)))
	}
	st.MinZoom = minZoom
	st.LastRender = time.Now().Unix()
	return s.saveState(serverID, d.Key, st)
}

func (s *Service) composeParent(serverID, dim string, z, x, y int) {
	out := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	found := false
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			child := readPNG(s.TilePath(serverID, dim, z+1, x*2+dx, y*2+dy))
			if child == nil {
				continue
			}
			found = true
			downscale(out, child, dx*256, dy*256)
		}
	}
	path := s.TilePath(serverID, dim, z, x, y)
	if !found {
		os.Remove(path)
		return
	}
	writePNG(path, out)
}

func readPNG(path string) *image.NRGBA {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil
	}
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	n := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			n.Set(x, y, img.At(x, y))
		}
	}
	return n
}

var encoder = png.Encoder{CompressionLevel: png.DefaultCompression}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := encoder.Encode(&buf, img); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Clear deletes all rendered tiles of a server.
func (s *Service) Clear(serverID string) error {
	return os.RemoveAll(filepath.Join(s.Dir, serverID))
}

// Dims lists the dimensions that have rendered tiles.
func (s *Service) Dims(serverID string) []string {
	ents, _ := os.ReadDir(filepath.Join(s.Dir, serverID))
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// TransparentPNG is served for missing tiles.
var TransparentPNG = func() []byte {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	return buf.Bytes()
}()
