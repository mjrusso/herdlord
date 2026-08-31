package target

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParsePrefix(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", []string{}},
		{`ssh workbox --`, []string{"ssh", "workbox", "--"}},
		{`ssh -i "key with spaces" host --`, []string{"ssh", "-i", "key with spaces", "host", "--"}},
		{`docker exec foo\ bar`, []string{"docker", "exec", "foo bar"}},
	}
	for _, tt := range tests {
		got, err := ParsePrefix(tt.input)
		if err != nil {
			t.Fatalf("ParsePrefix(%q): %v", tt.input, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParsePrefix(%q) = %#v, want %#v", tt.input, got, tt.want)
		}
	}
}

func TestMutateSerializesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	const writers = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Mutate(path, func(current []Target) ([]Target, error) {
				return append(current, Target{Name: fmt.Sprintf("target-%d", i)}), nil
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load(path)
	if err != nil || len(got) != writers {
		t.Fatalf("targets = %#v, %v", got, err)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "targets.json")
	want := []Target{{Name: "here", Prefix: []string{}}, {Name: "box", Prefix: []string{"ssh", "box", "--"}, Paused: true}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadMalformedJSONReportsDecodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	if err := os.WriteFile(path, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "decode targets:") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestFailedSavePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	if err := Save(path, []Target{{Name: "existing"}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, []Target{{Name: "duplicate"}, {Name: "duplicate"}}); err == nil {
		t.Fatal("Save accepted invalid targets")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("failed Save changed file from %q to %q", before, after)
	}
}

func TestSaveUsesPrivateFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	if err := Save(path, []Target{{Name: "private"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("targets mode = %o, want 600", got)
	}
}

func TestMutateSerializesAcrossProcesses(t *testing.T) {
	if os.Getenv("HERDLORD_LOCK_HELPER") != "" {
		runLockHelper(t)
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "targets.json")
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	acquired := filepath.Join(dir, "acquired")
	started := filepath.Join(dir, "started")
	first := lockHelperCommand(t, path, "first", "", ready, release, "")
	var firstOutput bytes.Buffer
	first.Stdout, first.Stderr = &firstOutput, &firstOutput
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, ready)
	second := lockHelperCommand(t, path, "second", started, "", "", acquired)
	var secondOutput bytes.Buffer
	second.Stdout, second.Stderr = &secondOutput, &secondOutput
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, started)
	t.Cleanup(func() {
		_ = first.Process.Kill()
		_ = second.Process.Kill()
	})
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(acquired); !os.IsNotExist(err) {
		t.Fatalf("second process acquired lock before release: %v", err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first helper: %v\n%s", err, firstOutput.Bytes())
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("second helper: %v\n%s", err, secondOutput.Bytes())
	}
	want := []Target{{Name: "first"}, {Name: "second"}}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v, want %#v, error = %v", got, want, err)
	}
}

func lockHelperCommand(t *testing.T, path, name, started, ready, release, acquired string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMutateSerializesAcrossProcesses$")
	cmd.Env = append(os.Environ(),
		"HERDLORD_LOCK_HELPER=1",
		"HERDLORD_LOCK_PATH="+path,
		"HERDLORD_LOCK_NAME="+name,
		"HERDLORD_LOCK_STARTED="+started,
		"HERDLORD_LOCK_READY="+ready,
		"HERDLORD_LOCK_RELEASE="+release,
		"HERDLORD_LOCK_ACQUIRED="+acquired,
	)
	return cmd
}

func runLockHelper(t *testing.T) {
	t.Helper()
	if started := os.Getenv("HERDLORD_LOCK_STARTED"); started != "" {
		if err := os.WriteFile(started, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Mutate(os.Getenv("HERDLORD_LOCK_PATH"), func(current []Target) ([]Target, error) {
		if acquired := os.Getenv("HERDLORD_LOCK_ACQUIRED"); acquired != "" {
			if err := os.WriteFile(acquired, nil, 0o600); err != nil {
				return nil, err
			}
		}
		if ready := os.Getenv("HERDLORD_LOCK_READY"); ready != "" {
			if err := os.WriteFile(ready, nil, 0o600); err != nil {
				return nil, err
			}
		}
		if release := os.Getenv("HERDLORD_LOCK_RELEASE"); release != "" {
			for {
				if _, err := os.Stat(release); err == nil {
					break
				} else if !os.IsNotExist(err) {
					return nil, err
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		return append(current, Target{Name: os.Getenv("HERDLORD_LOCK_NAME")}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestValidateRejectsSurroundingWhitespace(t *testing.T) {
	for _, name := range []string{" box", "box ", "\tbox"} {
		if err := Validate([]Target{{Name: name}}); err == nil {
			t.Fatalf("Validate accepted target name %q", name)
		}
	}
}
