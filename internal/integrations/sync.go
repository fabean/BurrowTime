package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

type Options struct {
	Connection   string
	From, To     time.Time // half-open selection by original entry start
	DryRun       bool
	Review       func([]Receipt) ([]Receipt, error)
	SkipProjects map[string]bool
}

func target(c Connection) string { return c.Plugin + ":" + c.WorkspaceID + ":" + c.UserID }

func Projection(f store.Frame, name string, c Connection, m Mapping) (Receipt, error) {
	if f.ID == "" || f.IDNull {
		return Receipt{}, fmt.Errorf("entry has no stable ID")
	}
	if f.Stop == nil || *f.Stop <= f.Start {
		return Receipt{}, fmt.Errorf("entry has no positive completed duration")
	}
	seconds := *f.Stop - f.Start
	if seconds <= 0 {
		return Receipt{}, fmt.Errorf("duration overflow")
	}
	rounding := c.Rounding
	if m.Rounding != nil {
		rounding = *m.Rounding
	}
	rounded, err := rounding.Seconds(seconds)
	if err != nil {
		return Receipt{}, err
	}
	if rounded == 0 {
		return Receipt{}, fmt.Errorf("rounding produces zero seconds; use up or off")
	}
	if f.Start > (1<<63-1)-rounded {
		return Receipt{}, fmt.Errorf("rounded end overflows")
	}
	tags := append([]string(nil), f.Tags...)
	sort.Strings(tags)
	for i, tag := range tags {
		tags[i] = strings.TrimPrefix(tag, "+")
	}
	description := strings.Join(tags, " ")
	if len([]rune(description)) > 3000 {
		return Receipt{}, fmt.Errorf("description exceeds Clockify's 3000-character limit")
	}
	entry := Entry{Start: time.Unix(f.Start, 0).UTC().Format(time.RFC3339), End: time.Unix(f.Start+rounded, 0).UTC().Format(time.RFC3339), ProjectID: m.ProjectID, Description: description, Billable: m.Billable}
	return Receipt{FrameID: f.ID, Connection: name, Target: target(c), Fingerprint: exportFingerprint(entry, seconds, rounding), Status: "pending", Entry: entry, RecordedSeconds: seconds, ExportSeconds: rounded}, nil
}

func exportFingerprint(entry Entry, seconds int64, rounding Rounding) string {
	// Include original duration, even when an edit rounds to the same export.
	data, _ := json.Marshal(struct {
		Entry           Entry
		OriginalSeconds int64
		Rounding        Rounding
	}{entry, seconds, rounding})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Accept both older description formats without changing their receipts or
// duplicating remote entries when upgrading the description format.
func matchesFingerprint(old Receipt, current Receipt, c Connection, m Mapping, f store.Frame) bool {
	if old.Fingerprint == current.Fingerprint {
		return true
	}
	entry := current.Entry
	tags := append([]string(nil), f.Tags...)
	sort.Strings(tags)
	entry.Description = f.Project
	for _, tag := range tags {
		entry.Description += " +" + tag
	}
	rounding := c.Rounding
	if m.Rounding != nil {
		rounding = *m.Rounding
	}
	if old.Fingerprint == exportFingerprint(entry, current.RecordedSeconds, rounding) {
		return true
	}
	entry.Description += " [burrowtime:" + current.FrameID + "]"
	return old.Fingerprint == exportFingerprint(entry, current.RecordedSeconds, rounding)
}

func ValidateConnection(c Connection) error {
	if c.Plugin != "clockify" {
		return fmt.Errorf("only the clockify connector is currently supported")
	}
	if strings.TrimSpace(c.WorkspaceID) == "" || strings.TrimSpace(c.UserID) == "" || strings.TrimSpace(c.APIKeyEnv) == "" {
		return fmt.Errorf("workspace ID, user ID, and API key environment variable are required")
	}
	_, err := c.Rounding.Seconds(1)
	return err
}

// Sync runs under the caller's exclusive lock. Receipts are saved before each
// create attempt, then immediately updated with the remote ID after success.
func Sync(ctx context.Context, dir string, config Config, frames []store.Frame, opts Options, call Caller, out io.Writer) error {
	c, ok := config.Connections[opts.Connection]
	if !ok {
		return fmt.Errorf("connection %q is not configured", opts.Connection)
	}
	if err := ValidateConnection(c); err != nil {
		return err
	}
	ledger, err := LoadLedger(dir)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, f := range frames {
		if f.ID != "" && !f.IDNull {
			if seen[f.ID] {
				return fmt.Errorf("duplicate local frame ID %s; fix the local data before exporting", f.ID)
			}
			seen[f.ID] = true
		}
	}
	var queue []Receipt
	unchanged, unmapped, blocked := 0, 0, 0
	for _, f := range frames {
		if f.Stop == nil {
			continue
		}
		start := time.Unix(f.Start, 0)
		if (!opts.From.IsZero() && start.Before(opts.From)) || (!opts.To.IsZero() && !start.Before(opts.To)) {
			continue
		}
		m, mapped := config.Projects[f.Project]
		if opts.SkipProjects[f.Project] {
			continue
		}
		if !mapped {
			unmapped++
			continue
		}
		if m.Connection != opts.Connection {
			continue
		}
		if m.ProjectID == "" {
			return fmt.Errorf("project %q has no remote mapping", f.Project)
		}
		r, err := Projection(f, opts.Connection, c, m)
		if err != nil {
			return fmt.Errorf("project %q, frame %s: %w", f.Project, f.ID, err)
		}
		if old, exists := ledger.Records[f.ID]; exists {
			switch {
			case old.Target != r.Target:
				fmt.Fprintf(out, "BLOCKED %s: previously exported or attempted for another destination\n", f.ID)
				blocked++
			case old.Status == "pending":
				fmt.Fprintf(out, "BLOCKED %s: earlier upload outcome is uncertain; reconcile it before retrying\n", f.ID)
				blocked++
			case old.Status == "retryable":
				queue = append(queue, r)
			case !matchesFingerprint(old, r, c, m, f):
				fmt.Fprintf(out, "BLOCKED %s: changed after export to %s; no duplicate created\n", f.ID, old.RemoteID)
				blocked++
			default:
				unchanged++
			}
			continue
		}
		queue = append(queue, r)
	}
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].Entry.Start == queue[j].Entry.Start {
			return queue[i].FrameID < queue[j].FrameID
		}
		return queue[i].Entry.Start < queue[j].Entry.Start
	})
	if len(queue) > 0 && opts.Review != nil && blocked == 0 && unmapped == 0 {
		queue, err = opts.Review(queue)
		if err != nil {
			return err
		}
	}
	for _, r := range queue {
		fmt.Fprintf(out, "%s %s: recorded %s -> export %s (adjustment %s)\n", r.FrameID, r.Entry.Description, time.Duration(r.RecordedSeconds)*time.Second, time.Duration(r.ExportSeconds)*time.Second, time.Duration(r.ExportSeconds-r.RecordedSeconds)*time.Second)
	}
	if opts.DryRun {
		fmt.Fprintf(out, "Dry run: %d to create, %d already synced, %d unmapped, %d blocked. No remote writes.\n", len(queue), unchanged, unmapped, blocked)
		if blocked > 0 || unmapped > 0 {
			return fmt.Errorf("%d blocked and %d unmapped entries need attention", blocked, unmapped)
		}
		return nil
	}
	if blocked > 0 || unmapped > 0 {
		return fmt.Errorf("resolve %d blocked and %d unmapped entries before uploading", blocked, unmapped)
	}
	created := 0
	if len(queue) > 0 {
		response, err := call(ctx, Request{Version: 1, Operation: "check", Connection: c})
		if err != nil {
			return err
		}
		if response.UserID != c.UserID {
			return fmt.Errorf("API key belongs to another Clockify user")
		}
		projects, err := call(ctx, Request{Version: 1, Operation: "projects", Connection: c})
		if err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, p := range projects.Projects {
			ids[p.ID] = true
		}
		for _, r := range queue {
			if !ids[r.Entry.ProjectID] {
				return fmt.Errorf("Clockify project %s is not accessible; fix the mapping before syncing", r.Entry.ProjectID)
			}
		}
		for _, r := range queue {
			ledger.Records[r.FrameID] = r
			if err := SaveLedger(dir, ledger); err != nil {
				return err
			}
			response, err := call(ctx, Request{Version: 1, Operation: "create", Connection: c, Entry: r.Entry})
			if err != nil {
				return fmt.Errorf("%d created; upload %s is recorded as pending and will not be repeated automatically: %w", created, r.FrameID, err)
			}
			if response.RemoteID == "" {
				return fmt.Errorf("missing remote ID for %s; kept pending to prevent duplicates", r.FrameID)
			}
			r.RemoteID = response.RemoteID
			r.Status = "synced"
			r.SyncedAt = time.Now().UTC().Format(time.RFC3339)
			ledger.Records[r.FrameID] = r
			if err := SaveLedger(dir, ledger); err != nil {
				return fmt.Errorf("remote entry %s created but receipt update failed; do not upload again: %w", r.RemoteID, err)
			}
			created++
			fmt.Fprintf(out, "Created %s -> %s\n", r.FrameID, r.RemoteID)
		}
	}
	fmt.Fprintf(out, "%d created, %d already synced, %d unmapped, %d blocked.\n", created, unchanged, unmapped, blocked)
	if blocked > 0 {
		return fmt.Errorf("%d entries need attention", blocked)
	}
	return nil
}

// Resolve attaches a verified remote entry to an uncertain local receipt.
func Resolve(ctx context.Context, dir, name, id, remote string, c Connection, call Caller) error {
	l, err := LoadLedger(dir)
	if err != nil {
		return err
	}
	r, ok := l.Records[id]
	if !ok || r.Status != "pending" {
		return fmt.Errorf("frame %s has no pending upload", id)
	}
	if r.Connection != name || r.Target != target(c) {
		return fmt.Errorf("receipt belongs to another connection")
	}
	response, err := call(ctx, Request{Version: 1, Operation: "get", Connection: c, RemoteID: remote})
	if err != nil {
		return err
	}
	if response.UserID != c.UserID || response.RemoteID != remote || response.Entry == nil || *response.Entry != r.Entry {
		return fmt.Errorf("remote entry does not match the pending export")
	}
	r.RemoteID = remote
	r.Status = "synced"
	r.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	l.Records[id] = r
	return SaveLedger(dir, l)
}
