package store

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Frame is encoded as Watson's positional six-value JSON array. Keeping the
// representation here prevents an innocent struct refactor from changing a
// user's years of data.
type Frame struct {
	Start            int64
	Stop             *int64
	Project          string
	ID               string
	IDNull           bool
	Tags             []string
	UpdatedAt        int64
	UpdatedAtMissing bool
	// Subsecond fields are transient projections used by Watson-compatible
	// reports. They are intentionally absent from the six-slot file encoding.
	StartMicros int
	StopMicros  int
}

func (f Frame) MarshalJSON() ([]byte, error) {
	tags := f.Tags
	if tags == nil {
		tags = []string{}
	}
	var id any = f.ID
	if f.IDNull {
		id = nil
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode([6]any{f.Start, f.Stop, f.Project, id, tags, f.UpdatedAt}); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}

func (f *Frame) UnmarshalJSON(data []byte) error {
	var row []json.RawMessage
	if err := json.Unmarshal(data, &row); err != nil {
		return fmt.Errorf("frame must be an array: %w", err)
	}
	if len(row) < 4 || len(row) > 6 {
		return fmt.Errorf("frame must contain 4 to 6 values, got %d", len(row))
	}
	if err := json.Unmarshal(row[0], &f.Start); err != nil {
		return fmt.Errorf("invalid frame start: %w", err)
	}
	if !bytes.Equal(row[1], []byte("null")) {
		var stop int64
		if err := json.Unmarshal(row[1], &stop); err != nil {
			return fmt.Errorf("invalid frame stop: %w", err)
		}
		f.Stop = &stop
	}
	if err := json.Unmarshal(row[2], &f.Project); err != nil {
		return fmt.Errorf("invalid frame project: %w", err)
	}
	if !bytes.Equal(row[3], []byte("null")) {
		if err := json.Unmarshal(row[3], &f.ID); err != nil {
			return fmt.Errorf("invalid frame id: %w", err)
		}
	} else {
		f.IDNull = true
	}
	f.Tags = []string{}
	if len(row) >= 5 && !bytes.Equal(row[4], []byte("null")) {
		if err := json.Unmarshal(row[4], &f.Tags); err != nil {
			return fmt.Errorf("invalid frame tags: %w", err)
		}
	}
	if len(row) == 6 && !bytes.Equal(row[5], []byte("null")) {
		if err := json.Unmarshal(row[5], &f.UpdatedAt); err != nil {
			return fmt.Errorf("invalid frame updated_at: %w", err)
		}
	} else {
		f.UpdatedAtMissing = true
	}
	return nil
}

type State struct {
	Project string   `json:"project"`
	Start   int64    `json:"start"`
	Tags    []string `json:"tags"`
}

func (s State) Running() bool { return s.Project != "" }

// ActiveTimer is BurrowTime's companion representation for timers beyond the
// single running state Watson can encode. Primary is transient: the primary
// timer remains in Watson's state file and is never written with extra fields.
type ActiveTimer struct {
	ID      string   `json:"id"`
	Project string   `json:"project"`
	Start   int64    `json:"start"`
	Tags    []string `json:"tags"`
	Primary bool     `json:"-"`
}

func (t ActiveTimer) Running() bool { return t.Project != "" }

func (t ActiveTimer) State() State {
	return State{Project: t.Project, Start: t.Start, Tags: append([]string(nil), t.Tags...)}
}
