package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct{ root string }

func New(root string) *Store { return &Store{root: root} }

func Key(now time.Time, sha, ext string) string {
	return fmt.Sprintf("%04d/%02d/%s%s", now.Year(), int(now.Month()), sha, ext)
}

func (s *Store) path(key string) (string, error) {
	clean := filepath.Clean(key)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid blob key %q", key)
	}
	return filepath.Join(s.root, clean), nil
}

func (s *Store) Put(_ context.Context, key string, data []byte) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

func (s *Store) Get(_ context.Context, key string) ([]byte, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}
