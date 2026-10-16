package sftpd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// LoadHostKey reads the server's host key, creating an Ed25519 key on first
// use. The key must stay the same, or clients warn about a changed host.
func LoadHostKey(path string) (ssh.Signer, error) {
	if b, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(b)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "hearth sftp host key")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// Fingerprint returns the SHA256 fingerprint of the host key, as clients
// like FileZilla or ssh show it.
func Fingerprint(s ssh.Signer) string {
	if s == nil {
		return ""
	}
	return ssh.FingerprintSHA256(s.PublicKey())
}
