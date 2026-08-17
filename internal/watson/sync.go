package watson

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fabean/BurrowTime/internal/store"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type remoteFrame struct {
	ID      string          `json:"id"`
	BeginAt json.RawMessage `json:"begin_at"`
	EndAt   json.RawMessage `json:"end_at"`
	Project string          `json:"project"`
	Tags    []string        `json:"tags"`
}
type pushFrame struct {
	ID      string   `json:"id"`
	BeginAt string   `json:"begin_at"`
	EndAt   string   `json:"end_at"`
	Project string   `json:"project"`
	Tags    []string `json:"tags"`
}

func (s *Service) Sync(client HTTPDoer) (pulled, pushed int, err error) {
	if client == nil {
		client = http.DefaultClient
	}
	base := s.Config.Get("backend", "url", "")
	token := s.Config.Get("backend", "token", "")
	if base == "" || token == "" {
		return 0, 0, errors.New("You must specify a remote URL (backend.url) and a token (backend.token) using the config command.")
	}
	lastSync, err := s.Repo.LoadLastSync()
	if err != nil {
		return 0, 0, err
	}
	lastPull := time.Now().UTC()
	if s.Now != nil {
		lastPull = s.Now().UTC()
	}
	incoming, err := s.pull(client, base, token, lastSync)
	if err != nil {
		return 0, 0, err
	}
	pulled = len(incoming)
	for _, remote := range incoming {
		id, err := normalizeUUID(remote.ID)
		if err != nil {
			return 0, 0, err
		}
		start, err := remoteTimestamp(remote.BeginAt)
		if err != nil {
			return 0, 0, err
		}
		stop, err := remoteTimestamp(remote.EndAt)
		if err != nil {
			return 0, 0, err
		}
		frame := store.Frame{Start: start, Stop: &stop, Project: remote.Project, ID: id, Tags: remote.Tags, UpdatedAt: s.Now().UTC().Unix()}
		replaced := false
		for i := range s.Frames {
			if s.Frames[i].ID == id {
				s.Frames[i] = frame
				replaced = true
				break
			}
		}
		if !replaced {
			s.Frames = append(s.Frames, frame)
		}
	}
	outgoing := []pushFrame{}
	for _, frame := range s.Frames {
		updated := time.Unix(frame.UpdatedAt, 0).UTC()
		if updated.After(time.Unix(lastSync, 0).UTC()) && updated.Before(lastPull) {
			if frame.Stop == nil {
				continue
			}
			urn, err := uuidURN(frame.ID)
			if err != nil {
				return pulled, 0, err
			}
			outgoing = append(outgoing, pushFrame{ID: urn, BeginAt: utcString(frame.Start), EndAt: utcString(*frame.Stop), Project: frame.Project, Tags: nonNilTags(frame.Tags)})
		}
	}
	if err := s.push(client, base, token, outgoing); err != nil {
		return pulled, 0, err
	}
	pushed = len(outgoing)
	if pulled > 0 {
		if err := s.Repo.SaveFrames(s.Frames); err != nil {
			return pulled, pushed, err
		}
	}
	if err := s.Repo.SaveLastSync(s.Now().UTC().Unix()); err != nil {
		return pulled, pushed, err
	}
	return pulled, pushed, nil
}

func (s *Service) pull(client HTTPDoer, base, token string, lastSync int64) ([]remoteFrame, error) {
	endpoint := strings.TrimRight(base, "/") + "/frames/"
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	when := time.Unix(lastSync, 0).UTC()
	if lastSync != 0 {
		loc := time.Local
		if tz := getTZ(); tz != "" {
			if loaded, e := time.LoadLocation(tz); e == nil {
				loc = loaded
			}
		}
		_, offset := s.Now().In(loc).Zone()
		when = time.Unix(lastSync, 0).In(time.FixedZone("local", offset))
	}
	query.Set("last_sync", when.Format("2006-01-02T15:04:05-07:00"))
	parsed.RawQuery = query.Encode()
	request, _ := http.NewRequest(http.MethodGet, parsed.String(), nil)
	setSyncHeaders(request, token)
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("Unable to reach the server.")
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("An error occurred with the remote server: %s", strings.TrimSpace(string(body)))
	}
	if len(bytes.TrimSpace(body)) == 0 || bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		return []remoteFrame{}, nil
	}
	var frames []remoteFrame
	if err := json.Unmarshal(body, &frames); err != nil {
		return nil, err
	}
	return frames, nil
}

func (s *Service) push(client HTTPDoer, base, token string, frames []pushFrame) error {
	body, err := marshalPushFrames(frames)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(base, "/") + "/frames/bulk/"
	request, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	setSyncHeaders(request, token)
	response, err := client.Do(request)
	if err != nil {
		return errors.New("Unable to reach the server.")
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("An error occurred with the remote server (status: %d). Response was:\n%s", response.StatusCode, string(responseBody))
	}
	return nil
}

func setSyncHeaders(request *http.Request, token string) {
	request.Header.Set("content-type", "application/json")
	request.Header.Set("Authorization", "Token "+token)
}
func remoteTimestamp(raw json.RawMessage) (int64, error) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		if value, err := strconv.ParseFloat(string(number), 64); err == nil {
			return int64(value), nil
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		return int64(value), nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return 0, err
	}
	return parsed.Unix(), nil
}
func normalizeUUID(value string) (string, error) {
	value = strings.TrimPrefix(strings.ToLower(value), "urn:uuid:")
	value = strings.ReplaceAll(value, "-", "")
	if len(value) != 32 {
		return "", fmt.Errorf("invalid UUID %q", value)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("invalid UUID %q", value)
	}
	return value, nil
}
func uuidURN(id string) (string, error) {
	id, err := normalizeUUID(id)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("urn:uuid:%s-%s-%s-%s-%s", id[:8], id[8:12], id[12:16], id[16:20], id[20:]), nil
}
func utcString(timestamp int64) string {
	return time.Unix(timestamp, 0).UTC().Format("2006-01-02T15:04:05-07:00")
}
func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}
func getTZ() string { return strings.TrimSpace(strings.Trim(os.Getenv("TZ"), "\x00")) }

func marshalPushFrames(frames []pushFrame) ([]byte, error) {
	if len(frames) == 0 {
		return []byte("[]"), nil
	}
	var b bytes.Buffer
	b.WriteByte('[')
	for i, frame := range frames {
		if i > 0 {
			b.WriteString(", ")
		}
		fields := []any{frame.ID, frame.BeginAt, frame.EndAt, frame.Project, frame.Tags}
		names := []string{"id", "begin_at", "end_at", "project", "tags"}
		b.WriteByte('{')
		for j, name := range names {
			if j > 0 {
				b.WriteString(", ")
			}
			key, _ := pythonCompactJSON(name)
			value, err := pythonCompactJSON(fields[j])
			if err != nil {
				return nil, err
			}
			b.Write(key)
			b.WriteString(": ")
			b.Write(value)
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

// requests uses Python json.dumps defaults for the sync body: non-ASCII is
// escaped, while HTML punctuation remains literal.
func pythonCompactJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded = bytes.ReplaceAll(encoded, []byte(`\u003c`), []byte("<"))
	encoded = bytes.ReplaceAll(encoded, []byte(`\u003e`), []byte(">"))
	encoded = bytes.ReplaceAll(encoded, []byte(`\u0026`), []byte("&"))
	var output bytes.Buffer
	for len(encoded) > 0 {
		r, size := utf8.DecodeRune(encoded)
		if r < utf8.RuneSelf {
			output.WriteByte(encoded[0])
			encoded = encoded[1:]
			continue
		}
		if r <= 0xffff {
			fmt.Fprintf(&output, `\u%04x`, r)
		} else {
			value := r - 0x10000
			fmt.Fprintf(&output, `\u%04x\u%04x`, 0xd800+(value>>10), 0xdc00+(value&0x3ff))
		}
		encoded = encoded[size:]
	}
	return output.Bytes(), nil
}
