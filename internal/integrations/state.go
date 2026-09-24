package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Receipt struct {
	FrameID         string `json:"frame_id"`
	Connection      string `json:"connection"`
	Target          string `json:"target"`
	Fingerprint     string `json:"fingerprint"`
	RemoteID        string `json:"remote_id,omitempty"`
	Status          string `json:"status"`
	Entry           Entry  `json:"entry"`
	RecordedSeconds int64  `json:"recorded_seconds"`
	ExportSeconds   int64  `json:"export_seconds"`
	SyncedAt        string `json:"synced_at,omitempty"`
}

type Ledger struct {
	Version int                `json:"version"`
	Records map[string]Receipt `json:"records"`
}

func LoadConfig(dir string) (Config, error) {
	c := Config{}
	found, err := read(filepath.Join(dir, "integrations.json"), &c)
	if err != nil {
		return c, err
	}
	if !found {
		return Config{Version: 1, Connections: map[string]Connection{}, Projects: map[string]Mapping{}}, nil
	}
	if c.Version != 1 || c.Connections == nil || c.Projects == nil {
		return c, fmt.Errorf("invalid integrations.json schema")
	}
	return c, nil
}

func SaveConfig(dir string, c Config) error { return write(filepath.Join(dir, "integrations.json"), c) }

func LoadLedger(dir string) (Ledger, error) {
	l := Ledger{}
	found, err := read(filepath.Join(dir, "integration-sync.json"), &l)
	if err != nil {
		return l, err
	}
	if !found {
		return Ledger{Version: 1, Records: map[string]Receipt{}}, nil
	}
	if l.Version != 1 || l.Records == nil {
		return l, fmt.Errorf("invalid integration-sync.json schema; refusing to risk duplicate uploads")
	}
	for id, r := range l.Records {
		validKey := id == r.FrameID || id == r.Connection+":"+r.FrameID
		if !validKey || r.Target == "" || r.Fingerprint == "" || (r.Status != "pending" && r.Status != "synced" && r.Status != "retryable") || (r.Status == "synced" && r.RemoteID == "") {
			return l, fmt.Errorf("invalid sync receipt for %q; refusing to risk duplicate uploads", id)
		}
	}
	return l, nil
}

func SaveLedger(dir string, l Ledger) error {
	return write(filepath.Join(dir, "integration-sync.json"), l)
}

func read(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return true, fmt.Errorf("invalid %s: %w", filepath.Base(path), err)
	}
	return true, nil
}

// Write a complete, flushed replacement; never truncate an existing receipt log.
func write(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".integration-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		dir, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer dir.Close()
		if err := dir.Sync(); err != nil {
			return err
		}
	}
	return nil
}

// RetryPending is an explicit operator override after confirming the provider
// did not create the entry. It cannot reset a successful receipt.
func RetryPending(dir, name, id string, c Connection) error {
	l, err := LoadLedger(dir)
	if err != nil {
		return err
	}
	key := receiptKey(c, name, id)
	r, ok := l.Records[key]
	if !ok || r.Status != "pending" {
		return fmt.Errorf("frame has no pending upload")
	}
	if r.Connection != name || r.Target != target(c) {
		return fmt.Errorf("receipt belongs to another connection")
	}
	r.Status = "retryable"
	l.Records[key] = r
	return SaveLedger(dir, l)
}

// A persistent lock fails closed after a killed process. The user must verify
// no sync is running before removing the lock; an age-based lease is unsafe.
func Lock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "integration-sync.lock")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot acquire %s: another sync may be running; after a crash, verify it has stopped before removing this lock: %w", path, err)
	}
	_, err = fmt.Fprintf(f, "pid=%d\n", os.Getpid())
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		return nil, fmt.Errorf("cannot write sync lock")
	}
	return func() { _ = os.Remove(path) }, nil
}
