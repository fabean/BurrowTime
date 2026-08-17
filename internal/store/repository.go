package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type Repository struct {
	Dir string
}

type activeTimersDocument struct {
	Version int           `json:"version"`
	Timers  []ActiveTimer `json:"timers"`
}

func WatsonDir() (string, error) {
	if dir := os.Getenv("WATSON_DIR"); dir != "" {
		return dir, nil
	}
	return applicationDir("watson")
}

// BurrowTimeDir returns BurrowTime's own default data directory. Its files use
// Watson's format, but keeping a separate home means trying BurrowTime never
// mutates an existing Watson installation unless the user explicitly imports
// or exports data.
func BurrowTimeDir() (string, error) {
	if dir := os.Getenv("BURROWTIME_DIR"); dir != "" {
		return dir, nil
	}
	return applicationDir("burrowtime")
}

func applicationDir(name string) (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", name), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, name), nil
}

func New(dir string) (*Repository, error) {
	if dir == "" {
		var err error
		dir, err = BurrowTimeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve BurrowTime directory: %w", err)
		}
	}
	return &Repository{Dir: dir}, nil
}

func (r *Repository) path(name string) string { return filepath.Join(r.Dir, name) }

func readJSON(path string, target any) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("Invalid JSON file %s: %w", path, err)
	}
	return nil
}

func (r *Repository) LoadFrames() ([]Frame, error) {
	return LoadFramesFile(r.path("frames"))
}

func LoadFramesFile(path string) ([]Frame, error) {
	frames := []Frame{}
	if err := readJSON(path, &frames); err != nil {
		return nil, err
	}
	return frames, nil
}

func (r *Repository) LoadState() (State, error) {
	var state State
	if err := readJSON(r.path("state"), &state); err != nil {
		return State{}, err
	}
	if state.Tags == nil {
		state.Tags = []string{}
	}
	return state, nil
}

// LoadActiveTimers reads BurrowTime's additive companion state. Watson ignores
// this file and continues to see its normal single state object.
func (r *Repository) LoadActiveTimers() ([]ActiveTimer, error) {
	document := activeTimersDocument{Version: 1, Timers: []ActiveTimer{}}
	if err := readJSON(r.path("active_timers"), &document); err != nil {
		return nil, err
	}
	if document.Version != 0 && document.Version != 1 {
		return nil, fmt.Errorf("unsupported active_timers version %d", document.Version)
	}
	for i := range document.Timers {
		if document.Timers[i].Tags == nil {
			document.Timers[i].Tags = []string{}
		}
	}
	return document.Timers, nil
}

func (r *Repository) LoadLastSync() (int64, error) {
	var value int64
	return value, readJSON(r.path("last_sync"), &value)
}

func (r *Repository) SaveFrames(frames []Frame) error { return r.saveJSON("frames", frames) }

func (r *Repository) SaveState(state State) error {
	if !state.Running() {
		return r.saveJSON("state", struct{}{})
	}
	if state.Tags == nil {
		state.Tags = []string{}
	}
	return r.saveJSON("state", state)
}

func (r *Repository) SaveActiveTimers(timers []ActiveTimer) error {
	copyTimers := make([]ActiveTimer, len(timers))
	copy(copyTimers, timers)
	for i := range copyTimers {
		copyTimers[i].Primary = false
		if copyTimers[i].Tags == nil {
			copyTimers[i].Tags = []string{}
		}
	}
	return r.saveJSON("active_timers", activeTimersDocument{Version: 1, Timers: copyTimers})
}

func (r *Repository) SaveLastSync(value int64) error { return r.saveJSON("last_sync", value) }
func (r *Repository) SaveConfig(data []byte) error   { return r.atomicSave("config", data) }

func (r *Repository) saveJSON(name string, value any) error {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", " ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return r.atomicSave(name, bytes.TrimSuffix(output.Bytes(), []byte("\n")))
}

func (r *Repository) atomicSave(name string, data []byte) error {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(r.Dir, "."+name+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}

	dest := r.path(name)
	backup := dest + ".bak"
	if _, statErr := os.Stat(dest); statErr == nil {
		_ = os.Remove(backup)
		if err = os.Rename(dest, backup); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err = os.Rename(tmpName, dest); err != nil {
		// Best effort restoration keeps a failed save from hiding valid data.
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	if dir, openErr := os.Open(r.Dir); openErr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	ok = true
	return nil
}
