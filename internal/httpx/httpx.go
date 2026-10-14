// Package httpx provides HTTP helpers for talking to external APIs
// (Mojang, PaperMC, Modrinth, Adoptium ...) with a proper User-Agent,
// caching and checksum-verified downloads.
package httpx

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hearth/internal/version"
)

type Client struct {
	HTTP    *http.Client
	Contact func() string

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	body []byte
	at   time.Time
}

func New(contact func() string) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 0},
		Contact: contact,
		cache:   map[string]cacheEntry{},
	}
}

func (c *Client) ua() string {
	contact := ""
	if c.Contact != nil {
		contact = c.Contact()
	}
	return version.UserAgent(contact)
}

type StatusError struct {
	Code int
	URL  string
	Body string
}

func (e *StatusError) Error() string {
	host := e.URL
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	return fmt.Sprintf("%s antwortete mit HTTP %d", host, e.Code)
}

func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == 404
}

// Get performs a GET request and returns the body (max 32 MiB).
// Responses are cached for ttl (0 = no caching).
func (c *Client) Get(ctx context.Context, url string, ttl time.Duration) ([]byte, error) {
	if ttl > 0 {
		c.mu.Lock()
		e, ok := c.cache[url]
		c.mu.Unlock()
		if ok && time.Since(e.at) < ttl {
			return e.body, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua())
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, friendly(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, &StatusError{Code: resp.StatusCode, URL: url, Body: string(body[:min(len(body), 300)])}
	}
	if ttl > 0 {
		c.mu.Lock()
		c.cache[url] = cacheEntry{body: body, at: time.Now()}
		c.mu.Unlock()
	}
	return body, nil
}

// GetJSON fetches and decodes JSON.
func (c *Client) GetJSON(ctx context.Context, url string, ttl time.Duration, v any) error {
	b, err := c.Get(ctx, url, ttl)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("ungültige Antwort von %s: %w", url, err)
	}
	return nil
}

// PostJSON sends a JSON body and decodes the JSON response.
func (c *Client) PostJSON(ctx context.Context, url string, body any, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua())
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return friendly(err)
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &StatusError{Code: resp.StatusCode, URL: url, Body: string(rb[:min(len(rb), 300)])}
	}
	return json.Unmarshal(rb, v)
}

func friendly(err error) error {
	var ue interface{ Timeout() bool }
	if errors.As(err, &ue) && ue.Timeout() {
		return fmt.Errorf("Zeitüberschreitung bei der Verbindung: %w", err)
	}
	return fmt.Errorf("Verbindung fehlgeschlagen: %w", err)
}

// Checksum describes an expected file hash ("sha1", "sha256" or "sha512").
type Checksum struct {
	Algo string
	Hex  string
}

// ProgressFunc receives downloaded and total bytes (total may be -1).
type ProgressFunc func(done, total int64)

// Download fetches url into dest atomically, verifying the checksum if given.
func (c *Client) Download(ctx context.Context, url, dest string, sum *Checksum, progress ProgressFunc) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return friendly(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &StatusError{Code: resp.StatusCode, URL: url}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".download"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var h hash.Hash
	if sum != nil && sum.Hex != "" {
		switch strings.ToLower(sum.Algo) {
		case "sha1":
			h = sha1.New()
		case "sha256":
			h = sha256.New()
		case "sha512":
			h = sha512.New()
		}
	}
	var w io.Writer = f
	if h != nil {
		w = io.MultiWriter(f, h)
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				f.Close()
				os.Remove(tmp)
				return err
			}
			done += int64(n)
			if progress != nil && time.Since(last) > 400*time.Millisecond {
				last = time.Now()
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return friendly(rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	if h != nil {
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, sum.Hex) {
			os.Remove(tmp)
			return fmt.Errorf("Prüfsumme stimmt nicht (%s erwartet %s, erhalten %s)", sum.Algo, sum.Hex, got)
		}
	}
	return os.Rename(tmp, dest)
}

// FileHash returns the hex hash of a file.
func FileHash(path, algo string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var h hash.Hash
	switch algo {
	case "sha1":
		h = sha1.New()
	case "sha512":
		h = sha512.New()
	default:
		h = sha256.New()
	}
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
