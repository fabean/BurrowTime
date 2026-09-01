package watson

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

const DefaultAgentLease = 30 * time.Minute

type AgentStartOptions struct {
	Client         string
	Owner          string
	Project        string
	Task           string
	Tags           []string
	Repository     string
	Branch         string
	IdempotencyKey string
	Lease          time.Duration
}

func (s *Service) StartAgentSession(options AgentStartOptions) (store.AgentSession, bool, error) {
	if _, err := s.RecoverExpiredAgentSessions(); err != nil {
		return store.AgentSession{}, false, err
	}
	options.Client = strings.ToLower(strings.TrimSpace(options.Client))
	options.Project = strings.TrimSpace(options.Project)
	options.Task = strings.TrimSpace(strings.TrimPrefix(options.Task, "+"))
	options.IdempotencyKey = strings.TrimSpace(options.IdempotencyKey)
	if options.Client == "" {
		return store.AgentSession{}, false, errors.New("No agent client given.")
	}
	if options.Project == "" {
		return store.AgentSession{}, false, errors.New("No project given.")
	}
	if options.Task == "" {
		return store.AgentSession{}, false, errors.New("No task or ticket given.")
	}
	if options.Lease == 0 {
		options.Lease = DefaultAgentLease
	}
	if options.Lease < time.Minute {
		return store.AgentSession{}, false, errors.New("Agent lease must be at least one minute.")
	}
	if options.IdempotencyKey != "" {
		for _, session := range s.AgentSessions {
			if session.Client == options.Client && session.IdempotencyKey == options.IdempotencyKey {
				if session.Project != options.Project || session.Task != options.Task {
					return store.AgentSession{}, false, fmt.Errorf("Agent idempotency key is already used by %s +%s.", session.Project, session.Task)
				}
				return session, true, nil
			}
		}
	}
	id, err := newID()
	if err != nil {
		return store.AgentSession{}, false, err
	}

	tags := make([]string, 0, len(options.Tags)+1)
	tags = append(tags, options.Task)
	for _, tag := range options.Tags {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "+"))
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	timer, err := s.StartConcurrent(options.Project, unique(tags), nil)
	if err != nil {
		return store.AgentSession{}, false, err
	}
	now := s.Now().Unix()
	session := store.AgentSession{
		ID: id, TimerID: timer.ID, Client: options.Client, Owner: strings.TrimSpace(options.Owner),
		Project: options.Project, Task: options.Task, Tags: append([]string(nil), timer.Tags...),
		Repository: strings.TrimSpace(options.Repository), Branch: strings.TrimSpace(options.Branch),
		IdempotencyKey: strings.TrimSpace(options.IdempotencyKey), Status: store.AgentSessionActive,
		StartedAt: timer.Start, HeartbeatAt: now, LeaseSeconds: int64(options.Lease / time.Second), FrameIDs: []string{},
	}
	sessions := append(append([]store.AgentSession(nil), s.AgentSessions...), session)
	if err := s.Repo.SaveAgentSessions(sessions); err != nil {
		if rollbackErr := s.rollbackStartedTimer(timer); rollbackErr != nil {
			return store.AgentSession{}, false, errors.Join(err, fmt.Errorf("roll back agent timer: %w", rollbackErr))
		}
		return store.AgentSession{}, false, err
	}
	s.AgentSessions = sessions
	return session, false, nil
}

func (s *Service) AgentSession(ref string) (store.AgentSession, int, error) {
	if ref == "" {
		return store.AgentSession{}, -1, errors.New("No agent session selected.")
	}
	match := -1
	for i, session := range s.AgentSessions {
		if session.ID == ref || strings.HasPrefix(session.ID, ref) {
			if match >= 0 {
				return store.AgentSession{}, -1, fmt.Errorf("Agent session id %s is ambiguous.", ref)
			}
			match = i
		}
	}
	if match < 0 {
		return store.AgentSession{}, -1, fmt.Errorf("No agent session found with id %s.", ref)
	}
	return s.AgentSessions[match], match, nil
}

func (s *Service) HeartbeatAgentSession(ref string) (store.AgentSession, error) {
	if _, err := s.RecoverExpiredAgentSessions(); err != nil {
		return store.AgentSession{}, err
	}
	session, index, err := s.AgentSession(ref)
	if err != nil {
		return store.AgentSession{}, err
	}
	if !session.Running() {
		return session, fmt.Errorf("Agent session %s is %s.", shortID(session.ID), session.Status)
	}
	session.HeartbeatAt = s.Now().Unix()
	sessions := append([]store.AgentSession(nil), s.AgentSessions...)
	sessions[index] = session
	if err := s.Repo.SaveAgentSessions(sessions); err != nil {
		return store.AgentSession{}, err
	}
	s.AgentSessions = sessions
	return session, nil
}

func (s *Service) rollbackStartedTimer(timer store.ActiveTimer) error {
	if timer.ID == PrimaryTimerID {
		if err := s.Repo.SaveState(store.State{}); err != nil {
			return err
		}
		s.State = store.State{}
		return nil
	}
	index := -1
	for i := range s.Concurrent {
		if s.Concurrent[i].ID == timer.ID {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("started timer %s is missing", shortID(timer.ID))
	}
	concurrent := append([]store.ActiveTimer(nil), s.Concurrent...)
	concurrent = append(concurrent[:index], concurrent[index+1:]...)
	if err := s.Repo.SaveActiveTimers(concurrent); err != nil {
		return err
	}
	s.Concurrent = concurrent
	return nil
}

func (s *Service) PauseAgentSession(ref string) (store.AgentSession, error) {
	session, _, err := s.AgentSession(ref)
	if err != nil {
		return store.AgentSession{}, err
	}
	if session.Status == store.AgentSessionPaused {
		return session, nil
	}
	if !session.Running() {
		return session, fmt.Errorf("Agent session %s is %s.", shortID(session.ID), session.Status)
	}
	if _, err := s.stopTimer(session.TimerID, nil, session.ID, store.AgentSessionPaused); err != nil {
		return store.AgentSession{}, err
	}
	updated, _, err := s.AgentSession(session.ID)
	return updated, err
}

func (s *Service) ResumeAgentSession(ref string) (store.AgentSession, error) {
	session, index, err := s.AgentSession(ref)
	if err != nil {
		return store.AgentSession{}, err
	}
	if session.Running() {
		return session, nil
	}
	if session.Status != store.AgentSessionPaused {
		return session, fmt.Errorf("Agent session %s is %s.", shortID(session.ID), session.Status)
	}
	timer, err := s.StartConcurrent(session.Project, session.Tags, nil)
	if err != nil {
		return store.AgentSession{}, err
	}
	session.TimerID = timer.ID
	session.Status = store.AgentSessionActive
	session.HeartbeatAt = s.Now().Unix()
	sessions := append([]store.AgentSession(nil), s.AgentSessions...)
	sessions[index] = session
	if err := s.Repo.SaveAgentSessions(sessions); err != nil {
		if rollbackErr := s.rollbackStartedTimer(timer); rollbackErr != nil {
			return store.AgentSession{}, errors.Join(err, fmt.Errorf("roll back resumed agent timer: %w", rollbackErr))
		}
		return store.AgentSession{}, err
	}
	s.AgentSessions = sessions
	return session, nil
}

func (s *Service) StopAgentSession(ref string) (store.AgentSession, bool, error) {
	session, index, err := s.AgentSession(ref)
	if err != nil {
		return store.AgentSession{}, false, err
	}
	if session.Status == store.AgentSessionPaused {
		now := s.Now().Unix()
		session.Status, session.StoppedAt, session.HeartbeatAt = store.AgentSessionStopped, &now, now
		sessions := append([]store.AgentSession(nil), s.AgentSessions...)
		sessions[index] = session
		if err := s.Repo.SaveAgentSessions(sessions); err != nil {
			return store.AgentSession{}, false, err
		}
		s.AgentSessions = sessions
		return session, false, nil
	}
	if !session.Running() {
		return session, true, nil
	}
	if _, err := s.stopTimer(session.TimerID, nil, session.ID, store.AgentSessionStopped); err != nil {
		return store.AgentSession{}, false, err
	}
	updated, _, err := s.AgentSession(session.ID)
	return updated, false, err
}

func (s *Service) RecoverExpiredAgentSessions() ([]store.AgentSession, error) {
	now := s.Now().Unix()
	var recovered []store.AgentSession
	for _, candidate := range append([]store.AgentSession(nil), s.AgentSessions...) {
		current, _, err := s.AgentSession(candidate.ID)
		if err != nil {
			return recovered, err
		}
		if !current.Running() || current.LeaseSeconds <= 0 || current.HeartbeatAt+current.LeaseSeconds > now {
			continue
		}
		expires := time.Unix(current.HeartbeatAt+current.LeaseSeconds, 0).In(s.Now().Location())
		if _, err := s.stopTimer(current.TimerID, &expires, current.ID, store.AgentSessionExpired); err != nil {
			return recovered, err
		}
		updated, _, err := s.AgentSession(current.ID)
		if err != nil {
			return recovered, err
		}
		recovered = append(recovered, updated)
	}
	return recovered, nil
}

func stoppedAgentSessions(sessions []store.AgentSession, timerID, promotedTimerID string, frame store.Frame, stoppedAt int64, sessionID string, status store.AgentSessionStatus) ([]store.AgentSession, bool, error) {
	out := append([]store.AgentSession(nil), sessions...)
	changed, matched := false, false
	for i := range out {
		if out[i].Running() && promotedTimerID != "" && out[i].TimerID == promotedTimerID {
			out[i].TimerID = PrimaryTimerID
			changed = true
		}
		selected := out[i].Running() && out[i].TimerID == timerID
		if sessionID != "" {
			selected = out[i].ID == sessionID && out[i].Running() && out[i].TimerID == timerID
		}
		if !selected {
			continue
		}
		matched = true
		out[i].FrameIDs = append(append([]string(nil), out[i].FrameIDs...), frame.ID)
		out[i].TimerID = ""
		out[i].HeartbeatAt = stoppedAt
		out[i].Status = status
		if status != store.AgentSessionPaused {
			stop := stoppedAt
			out[i].StoppedAt = &stop
		}
		changed = true
	}
	if sessionID != "" && !matched {
		return sessions, false, fmt.Errorf("Agent session %s does not own timer %s.", shortID(sessionID), shortID(timerID))
	}
	return out, changed, nil
}

func canceledAgentSessions(sessions []store.AgentSession, timerID, promotedTimerID string, stoppedAt int64) ([]store.AgentSession, bool) {
	out := append([]store.AgentSession(nil), sessions...)
	changed := false
	for i := range out {
		if out[i].Running() && promotedTimerID != "" && out[i].TimerID == promotedTimerID {
			out[i].TimerID = PrimaryTimerID
			changed = true
		}
		if out[i].Running() && out[i].TimerID == timerID {
			stop := stoppedAt
			out[i].TimerID = ""
			out[i].Status = store.AgentSessionCanceled
			out[i].HeartbeatAt = stoppedAt
			out[i].StoppedAt = &stop
			changed = true
		}
	}
	return out, changed
}
