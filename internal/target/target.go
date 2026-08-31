package target

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-shellwords"
	"golang.org/x/sys/unix"
)

type Target struct {
	Name        string   `json:"name"`
	Prefix      []string `json:"prefix"`
	Interactive []string `json:"interactivePrefix,omitempty"`
	Paused      bool     `json:"paused,omitempty"`
}

func (t Target) InteractivePrefix() []string {
	if t.Interactive != nil {
		return t.Interactive
	}
	return t.Prefix
}

func ParsePrefix(input string) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return []string{}, nil
	}
	p := shellwords.NewParser()
	p.ParseEnv = false
	p.ParseBacktick = false
	words, err := p.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("parse prefix: %w", err)
	}
	return words, nil
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(dir, "herdlord", "targets.json"), nil
}

func Load(path string) ([]Target, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Target{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}
	var targets []Target
	if err := json.Unmarshal(b, &targets); err != nil {
		return nil, fmt.Errorf("decode targets: %w", err)
	}
	if err := Validate(targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func Save(path string, targets []Target) error {
	if err := Validate(targets); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	b, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return fmt.Errorf("encode targets: %w", err)
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".targets-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary targets file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write targets: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync targets: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close targets: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace targets: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open targets directory: %w", err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("sync targets directory: %w", err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("close targets directory: %w", err)
	}
	return nil
}

func Mutate(path string, change func([]Target) ([]Target, error)) ([]Target, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open targets lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return nil, fmt.Errorf("lock targets: %w", err)
	}
	defer func() { _ = unix.Flock(int(lock.Fd()), unix.LOCK_UN) }()
	current, err := Load(path)
	if err != nil {
		return nil, err
	}
	updated, err := change(current)
	if err != nil {
		return nil, err
	}
	if err := Save(path, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func Validate(targets []Target) error {
	seen := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		trimmed := strings.TrimSpace(t.Name)
		if trimmed == "" {
			return errors.New("target name cannot be empty")
		}
		if trimmed != t.Name {
			return fmt.Errorf("target name %q cannot begin or end with whitespace", t.Name)
		}
		if _, ok := seen[t.Name]; ok {
			return fmt.Errorf("duplicate target name %q", t.Name)
		}
		seen[t.Name] = struct{}{}
	}
	return nil
}
