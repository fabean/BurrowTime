package watson

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/config"
	"github.com/fabean/BurrowTime/internal/store"
)

type Service struct {
	Repo          *store.Repository
	Config        *config.Config
	Now           func() time.Time
	Frames        []store.Frame
	State         store.State
	Concurrent    []store.ActiveTimer
	AgentSessions []store.AgentSession
	Messages      store.SessionMessages
}

const PrimaryTimerID = "primary"

func Open(dir string) (*Service, error) {
	s, err := OpenData(dir)
	if err != nil {
		return nil, err
	}
	if err := s.LoadConfig(); err != nil {
		return nil, err
	}
	return s, nil
}

// OpenData loads compatible frame and running-state files without touching the
// lazily accessed configuration file.
func OpenData(dir string) (*Service, error) {
	repo, err := store.New(dir)
	if err != nil {
		return nil, err
	}
	frames, err := repo.LoadFrames()
	if err != nil {
		return nil, err
	}
	now := time.Now
	for i := range frames {
		if frames[i].UpdatedAtMissing {
			frames[i].UpdatedAt = now().UTC().Unix()
			frames[i].UpdatedAtMissing = false
		}
	}
	state, err := repo.LoadState()
	if err != nil {
		return nil, err
	}
	concurrent, err := repo.LoadActiveTimers()
	if err != nil {
		return nil, err
	}
	agentSessions, err := repo.LoadAgentSessions()
	if err != nil {
		return nil, err
	}
	messages, err := repo.LoadSessionMessages()
	if err != nil {
		return nil, err
	}
	for i := range frames {
		frames[i].Message = messages.Frames[frames[i].ID]
	}
	for i := range concurrent {
		concurrent[i].Message = messages.Timers[concurrent[i].ID]
	}
	return &Service{Repo: repo, Config: config.New(), Frames: frames, State: state, Concurrent: concurrent, AgentSessions: agentSessions, Messages: messages, Now: now}, nil
}

func (s *Service) SetFrameMessage(ref, message string) (store.Frame, error) {
	f, i, err := s.Lookup(ref)
	if err != nil {
		return store.Frame{}, err
	}
	f.Message = message
	if strings.TrimSpace(message) == "" {
		delete(s.Messages.Frames, f.ID)
	} else {
		s.Messages.Frames[f.ID] = message
	}
	if err := s.Repo.SaveSessionMessages(s.Messages); err != nil {
		return store.Frame{}, err
	}
	s.Frames[i].Message = message
	return s.Frames[i], nil
}

func (s *Service) SetTimerMessage(ref, message string) error {
	timer, index, err := s.resolveTimer(ref)
	if err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		delete(s.Messages.Timers, timer.ID)
	} else {
		s.Messages.Timers[timer.ID] = message
	}
	if err := s.Repo.SaveSessionMessages(s.Messages); err != nil {
		return err
	}
	if index < 0 {
		return nil
	}
	s.Concurrent[index].Message = message
	return nil
}

func (s *Service) LoadConfig() error {
	cfg, err := config.Load(filepath.Join(s.Repo.Dir, "config"))
	if err != nil {
		return fmt.Errorf("Cannot parse config file: %w", err)
	}
	s.Config = cfg
	return nil
}

func unique(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// RunningTimers returns the Watson primary state followed by BurrowTime's
// additional concurrent timers. The reserved ID "primary" is stable for the
// lifetime of the primary state and can be passed to StopTimer.
func (s *Service) RunningTimers() []store.ActiveTimer {
	out := make([]store.ActiveTimer, 0, len(s.Concurrent)+1)
	if s.State.Running() {
		out = append(out, store.ActiveTimer{ID: PrimaryTimerID, Project: s.State.Project, Start: s.State.Start, Tags: append([]string(nil), s.State.Tags...), Message: s.Messages.Timers[PrimaryTimerID], Primary: true})
	}
	for _, timer := range s.Concurrent {
		copyTimer := timer
		copyTimer.Tags = append([]string(nil), timer.Tags...)
		out = append(out, copyTimer)
	}
	return out
}

func (s *Service) HasRunningTimers() bool {
	return s.State.Running() || len(s.Concurrent) > 0
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) Start(project string, tags []string, at *time.Time, gap bool, restart bool) (store.State, error) {
	if strings.TrimSpace(project) == "" {
		return store.State{}, errors.New("No project given.")
	}
	if s.State.Running() {
		return store.State{}, fmt.Errorf("Project %s is already started.", s.State.Project)
	}
	now := s.Now()
	start := now
	if at != nil {
		start = *at
	}
	if start.After(now) {
		return store.State{}, errors.New("Task cannot start in the future.")
	}
	if !gap && len(s.Frames) == 0 {
		return store.State{}, errors.New("No previous frame exists; cannot use --no-gap.")
	}
	if len(s.Frames) > 0 {
		previous := s.Frames[len(s.Frames)-1]
		if !gap && previous.Stop != nil {
			start = time.Unix(*previous.Stop, 0).In(now.Location())
		}
		if at != nil && previous.Stop != nil && start.Unix() < *previous.Stop {
			return store.State{}, errors.New("Task cannot start before the previous task ends.")
		}
	}
	if !restart {
		tags = append(tags, s.Config.List("default_tags", project)...)
	}
	s.State = store.State{Project: project, Start: start.Unix(), Tags: unique(tags)}
	if err := s.Repo.SaveState(s.State); err != nil {
		return store.State{}, err
	}
	return s.State, nil
}

// StartConcurrent starts an additional timer without altering Watson's primary
// state. If no primary exists, the new timer becomes the primary so Watson can
// still see one running project.
func (s *Service) StartConcurrent(project string, tags []string, at *time.Time) (store.ActiveTimer, error) {
	if strings.TrimSpace(project) == "" {
		return store.ActiveTimer{}, errors.New("No project given.")
	}
	now := s.Now()
	start := now
	if at != nil {
		start = *at
	}
	if start.After(now) {
		return store.ActiveTimer{}, errors.New("Task cannot start in the future.")
	}
	tags = unique(append(tags, s.Config.List("default_tags", project)...))
	if !s.State.Running() {
		s.State = store.State{Project: project, Start: start.Unix(), Tags: append([]string(nil), tags...)}
		if err := s.Repo.SaveState(s.State); err != nil {
			return store.ActiveTimer{}, err
		}
		return store.ActiveTimer{ID: PrimaryTimerID, Project: project, Start: start.Unix(), Tags: tags, Primary: true}, nil
	}
	id, err := newID()
	if err != nil {
		return store.ActiveTimer{}, err
	}
	timer := store.ActiveTimer{ID: id, Project: project, Start: start.Unix(), Tags: tags}
	concurrent := append(append([]store.ActiveTimer(nil), s.Concurrent...), timer)
	if err := s.Repo.SaveActiveTimers(concurrent); err != nil {
		return store.ActiveTimer{}, err
	}
	s.Concurrent = concurrent
	return timer, nil
}

func (s *Service) Stop(at *time.Time) (store.Frame, error) {
	if !s.State.Running() {
		return store.Frame{}, errors.New("No project started.")
	}
	return s.StopTimer(PrimaryTimerID, at)
}

// StopTimer stops one primary or concurrent timer. ID prefixes are accepted as
// long as they identify exactly one running timer.
func (s *Service) StopTimer(ref string, at *time.Time) (store.Frame, error) {
	return s.stopTimer(ref, at, "", store.AgentSessionManuallyStopped)
}

func (s *Service) stopTimer(ref string, at *time.Time, sessionID string, status store.AgentSessionStatus) (store.Frame, error) {
	timer, index, err := s.resolveTimer(ref)
	if err != nil {
		return store.Frame{}, err
	}
	stop := s.Now()
	if at != nil {
		stop = *at
	}
	frame, err := s.stoppedFrame(timer, stop)
	if err != nil {
		return store.Frame{}, err
	}
	if message := s.Messages.Timers[timer.ID]; message != "" {
		frame.Message = message
		s.Messages.Frames[frame.ID] = message
		delete(s.Messages.Timers, timer.ID)
	}
	frames := append(append([]store.Frame(nil), s.Frames...), frame)
	if err := s.Repo.SaveFrames(frames); err != nil {
		return store.Frame{}, err
	}
	if frame.Message != "" {
		if err := s.Repo.SaveSessionMessages(s.Messages); err != nil {
			return store.Frame{}, err
		}
	}
	concurrent := append([]store.ActiveTimer(nil), s.Concurrent...)
	state := s.State
	promotedTimerID := ""
	if index < 0 {
		state = store.State{}
		if len(concurrent) > 0 {
			promotedTimerID = concurrent[0].ID
			state = concurrent[0].State()
			concurrent = concurrent[1:]
		}
		if err := s.Repo.SaveState(state); err != nil {
			return store.Frame{}, err
		}
	} else {
		concurrent = append(concurrent[:index], concurrent[index+1:]...)
	}
	if len(s.Concurrent) > 0 {
		if err := s.Repo.SaveActiveTimers(concurrent); err != nil {
			return store.Frame{}, err
		}
	}
	sessions, sessionsChanged, sessionErr := stoppedAgentSessions(s.AgentSessions, timer.ID, promotedTimerID, frame, stop.Unix(), sessionID, status)
	if sessionErr != nil {
		return store.Frame{}, sessionErr
	}
	if sessionsChanged {
		if err := s.Repo.SaveAgentSessions(sessions); err != nil {
			return store.Frame{}, err
		}
	}
	s.Frames, s.State, s.Concurrent, s.AgentSessions = frames, state, concurrent, sessions
	return frame, nil
}

// StopAll stops every running timer at one shared timestamp and persists all
// resulting frames in a single frames-file update.
func (s *Service) StopAll(at *time.Time) ([]store.Frame, error) {
	return s.stopAll(at, false)
}

// StopAllMinute stops human timers at the supplied minute boundary while
// preserving the current timestamp for agent-owned timers.
func (s *Service) StopAllMinute(at *time.Time) ([]store.Frame, error) {
	return s.stopAll(at, true)
}

func (s *Service) stopAll(at *time.Time, minuteMode bool) ([]store.Frame, error) {
	timers := s.RunningTimers()
	if len(timers) == 0 {
		return nil, errors.New("No project started.")
	}
	stop := s.Now()
	if at != nil {
		stop = *at
	}
	stopped := make([]store.Frame, 0, len(timers))
	for _, timer := range timers {
		stopAt := stop
		agentOwned := false
		for _, session := range s.AgentSessions {
			if session.TimerID == timer.ID && session.Running() {
				agentOwned = true
				break
			}
		}
		if minuteMode && agentOwned {
			stopAt = s.Now()
		} else if !agentOwned && stop.Unix() < timer.Start {
			started := time.Unix(timer.Start, 0).In(stop.Location())
			delta := timer.Start - stop.Unix()
			sy, sm, sd := started.Date()
			y, m, d := stop.Date()
			if delta < 60 && sy == y && sm == m && sd == d && started.Hour() == stop.Hour() && started.Minute() == stop.Minute() {
				stopAt = started
			}
		}
		frame, err := s.stoppedFrame(timer, stopAt)
		if err != nil {
			return nil, err
		}
		if message := s.Messages.Timers[timer.ID]; message != "" {
			frame.Message = message
			s.Messages.Frames[frame.ID] = message
			delete(s.Messages.Timers, timer.ID)
		}
		stopped = append(stopped, frame)
	}
	frames := append(append([]store.Frame(nil), s.Frames...), stopped...)
	if err := s.Repo.SaveFrames(frames); err != nil {
		return nil, err
	}
	if err := s.Repo.SaveSessionMessages(s.Messages); err != nil {
		return nil, err
	}
	if err := s.Repo.SaveState(store.State{}); err != nil {
		return nil, err
	}
	if len(s.Concurrent) > 0 {
		if err := s.Repo.SaveActiveTimers(nil); err != nil {
			return nil, err
		}
	}
	sessions := append([]store.AgentSession(nil), s.AgentSessions...)
	sessionsChanged := false
	for i, timer := range timers {
		updated, changed, updateErr := stoppedAgentSessions(sessions, timer.ID, "", stopped[i], *stopped[i].Stop, "", store.AgentSessionManuallyStopped)
		if updateErr != nil {
			return nil, updateErr
		}
		sessions, sessionsChanged = updated, sessionsChanged || changed
	}
	if sessionsChanged {
		if err := s.Repo.SaveAgentSessions(sessions); err != nil {
			return nil, err
		}
	}
	s.Frames, s.State, s.Concurrent, s.AgentSessions = frames, store.State{}, []store.ActiveTimer{}, sessions
	return stopped, nil
}

func (s *Service) resolveTimer(ref string) (store.ActiveTimer, int, error) {
	if ref == "" {
		return store.ActiveTimer{}, 0, errors.New("No timer selected.")
	}
	matches := make([]struct {
		timer store.ActiveTimer
		index int
	}, 0, 1)
	if s.State.Running() && strings.HasPrefix(PrimaryTimerID, ref) {
		matches = append(matches, struct {
			timer store.ActiveTimer
			index int
		}{s.RunningTimers()[0], -1})
	}
	for i, timer := range s.Concurrent {
		if timer.ID == ref || strings.HasPrefix(timer.ID, ref) {
			matches = append(matches, struct {
				timer store.ActiveTimer
				index int
			}{timer, i})
		}
	}
	if len(matches) == 0 {
		return store.ActiveTimer{}, 0, fmt.Errorf("No running timer found with id %s.", ref)
	}
	if len(matches) > 1 {
		return store.ActiveTimer{}, 0, fmt.Errorf("Running timer id %s is ambiguous.", ref)
	}
	return matches[0].timer, matches[0].index, nil
}

func (s *Service) stoppedFrame(timer store.ActiveTimer, stop time.Time) (store.Frame, error) {
	if stop.Unix() < timer.Start {
		return store.Frame{}, errors.New("Task cannot end before it starts.")
	}
	if stop.After(s.Now()) {
		return store.Frame{}, errors.New("Task cannot end in the future.")
	}
	id, err := newID()
	if err != nil {
		return store.Frame{}, err
	}
	return store.Frame{Start: timer.Start, Stop: ptr(stop.Unix()), Project: timer.Project, ID: id, Tags: append([]string(nil), timer.Tags...), UpdatedAt: s.Now().UTC().Unix()}, nil
}

func (s *Service) Add(project string, tags []string, from, to time.Time) (store.Frame, error) {
	if strings.TrimSpace(project) == "" {
		return store.Frame{}, errors.New("No project given.")
	}
	if from.After(to) {
		return store.Frame{}, errors.New("Task cannot end before it starts.")
	}
	id, err := newID()
	if err != nil {
		return store.Frame{}, err
	}
	tags = append(tags, s.Config.List("default_tags", project)...)
	f := store.Frame{Start: from.Unix(), Stop: ptr(to.Unix()), Project: project, ID: id, Tags: tags, UpdatedAt: s.Now().UTC().Unix()}
	s.Frames = append(s.Frames, f)
	if err := s.Repo.SaveFrames(s.Frames); err != nil {
		return store.Frame{}, err
	}
	if f.Message != "" {
		s.Messages.Frames[f.ID] = f.Message
		if err := s.Repo.SaveSessionMessages(s.Messages); err != nil {
			return store.Frame{}, err
		}
	}
	return f, nil
}

func (s *Service) Cancel() (store.State, error) {
	if !s.State.Running() {
		return store.State{}, errors.New("No project started.")
	}
	old := s.State
	state := store.State{}
	concurrent := append([]store.ActiveTimer(nil), s.Concurrent...)
	promotedTimerID := ""
	if len(concurrent) > 0 {
		promotedTimerID = concurrent[0].ID
		state = concurrent[0].State()
		concurrent = concurrent[1:]
	}
	if err := s.Repo.SaveState(state); err != nil {
		return store.State{}, err
	}
	if len(s.Concurrent) > 0 {
		if err := s.Repo.SaveActiveTimers(concurrent); err != nil {
			return store.State{}, err
		}
	}
	sessions, changed := canceledAgentSessions(s.AgentSessions, PrimaryTimerID, promotedTimerID, s.Now().Unix())
	if changed {
		if err := s.Repo.SaveAgentSessions(sessions); err != nil {
			return store.State{}, err
		}
	}
	s.State, s.Concurrent, s.AgentSessions = state, concurrent, sessions
	return old, nil
}

func (s *Service) Lookup(ref string) (store.Frame, int, error) {
	if index, err := strconv.Atoi(ref); err == nil && index < 0 {
		index = len(s.Frames) + index
		if index < 0 || index >= len(s.Frames) {
			return store.Frame{}, -1, fmt.Errorf("No frame found for index %s.", ref)
		}
		return s.Frames[index], index, nil
	}
	for i, f := range s.Frames {
		if strings.HasPrefix(f.ID, ref) {
			return f, i, nil
		}
	}
	return store.Frame{}, -1, fmt.Errorf("No frame found with id %s.", shortID(ref))
}

func (s *Service) Remove(ref string) (store.Frame, error) {
	f, i, err := s.Lookup(ref)
	if err != nil {
		return store.Frame{}, err
	}
	s.Frames = append(s.Frames[:i], s.Frames[i+1:]...)
	if err := s.Repo.SaveFrames(s.Frames); err != nil {
		return store.Frame{}, err
	}
	delete(s.Messages.Frames, f.ID)
	if err := s.Repo.SaveSessionMessages(s.Messages); err != nil {
		return store.Frame{}, err
	}
	return f, nil
}

func (s *Service) EditFrame(ref, project string, tags []string, start, stop time.Time) (store.Frame, error) {
	f, index, err := s.Lookup(ref)
	if err != nil {
		return store.Frame{}, err
	}
	if start.After(stop) {
		return store.Frame{}, errors.New("Task cannot end before it starts.")
	}
	now := s.Now()
	if start.After(now) {
		return store.Frame{}, errors.New("Start time cannot be in the future")
	}
	if stop.After(now) {
		return store.Frame{}, errors.New("Stop time cannot be in the future")
	}
	f.Project = project
	f.Tags = append([]string(nil), tags...)
	f.Start = start.Unix()
	stopUnix := stop.Unix()
	f.Stop = &stopUnix
	f.UpdatedAt = now.UTC().Unix()
	s.Frames[index] = f
	if err := s.Repo.SaveFrames(s.Frames); err != nil {
		return store.Frame{}, err
	}
	return f, nil
}

func (s *Service) EditCurrent(project string, tags []string, start time.Time) (store.State, error) {
	if !s.State.Running() {
		return store.State{}, errors.New("No project started.")
	}
	if start.After(s.Now()) {
		return store.State{}, errors.New("Start time cannot be in the future")
	}
	s.State = store.State{Project: project, Tags: append([]string(nil), tags...), Start: start.Unix()}
	if err := s.Repo.SaveState(s.State); err != nil {
		return store.State{}, err
	}
	return s.State, nil
}

func (s *Service) MergeReport(path string) (conflicting, merging []store.Frame, err error) {
	incoming, err := store.LoadFramesFile(path)
	if err != nil {
		return nil, nil, err
	}
	for _, candidate := range incoming {
		if candidate.UpdatedAtMissing {
			candidate.UpdatedAt = s.Now().UTC().Unix()
			candidate.UpdatedAtMissing = false
		}
		found := false
		for _, original := range s.Frames {
			if original.ID == candidate.ID {
				found = true
				if !reflect.DeepEqual(original, candidate) {
					conflicting = append(conflicting, candidate)
				}
				break
			}
		}
		if !found {
			merging = append(merging, candidate)
		}
	}
	return conflicting, merging, nil
}

func (s *Service) ApplyMerge(conflicts map[string]store.Frame, merging []store.Frame) error {
	for id, replacement := range conflicts {
		for i := range s.Frames {
			if s.Frames[i].ID == id {
				s.Frames[i] = replacement
				break
			}
		}
	}
	s.Frames = append(s.Frames, merging...)
	return s.Repo.SaveFrames(s.Frames)
}

func (s *Service) Rename(kind, oldName, newName string) error {
	now := s.Now().UTC().Unix()
	found := false
	for i := range s.Frames {
		switch kind {
		case "project":
			if s.Frames[i].Project == oldName {
				s.Frames[i].Project = newName
				s.Frames[i].UpdatedAt = now
				found = true
			}
		case "tag":
			for j, tag := range s.Frames[i].Tags {
				if tag == oldName {
					s.Frames[i].Tags[j] = newName
					s.Frames[i].UpdatedAt = now
					found = true
				}
			}
		default:
			return errors.New("TYPE must be either project or tag")
		}
	}
	if !found {
		label := kind
		if kind != "" {
			label = strings.ToUpper(kind[:1]) + kind[1:]
		}
		return fmt.Errorf("%s %q does not exist", label, oldName)
	}
	return s.Repo.SaveFrames(s.Frames)
}

func (s *Service) Projects() []string {
	set := map[string]bool{}
	for _, f := range s.Frames {
		set[f.Project] = true
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (s *Service) Tags() []string {
	set := map[string]bool{}
	for _, f := range s.Frames {
		for _, tag := range f.Tags {
			set[tag] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func ptr(v int64) *int64 { return &v }
func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}
