package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Job struct {
	ID           string `json:"id"`
	Schedule     string `json:"schedule"`
	AgentID      string `json:"agentID"`
	Message      string `json:"message"`
	Mode         string `json:"mode"`
	NotifyTarget string `json:"notifyTarget"`
	Enabled      bool   `json:"enabled"`
	LastRun      string `json:"lastRun"`
}

type RunFunc func(agentID string, message string)

var ErrJobNotFound = errors.New("scheduler: job not found")

type Store struct {
	path string

	mu   sync.Mutex
	jobs map[string]Job
}

type persistedJobs struct {
	Jobs []Job `json:"jobs"`
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("scheduler: path is required")
	}
	s := &Store{path: path, jobs: make(map[string]Job)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Add(job Job) error {
	if job.ID == "" {
		return errors.New("scheduler: job id is required")
	}
	if job.Schedule == "" {
		return errors.New("scheduler: job schedule is required")
	}
	if _, _, err := nextDue(job, time.Now().UTC()); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return s.saveLocked()
}

func (s *Store) List() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})
	return jobs
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return ErrJobNotFound
	}
	delete(s.jobs, id)
	return s.saveLocked()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("scheduler: read store: %w", err)
	}
	var p persistedJobs
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("scheduler: parse store: %w", err)
	}
	for _, job := range p.Jobs {
		s.jobs[job.ID] = job
	}
	return nil
}

func (s *Store) saveLocked() error {
	jobs := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	body, err := json.MarshalIndent(persistedJobs{Jobs: jobs}, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduler: encode store: %w", err)
	}
	body = append(body, '\n')

	return atomicWriteFile(s.path, body, 0o600)
}

func (s *Store) updateAfterRun(job Job, runAt time.Time, disable bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.jobs[job.ID]
	if !ok {
		return ErrJobNotFound
	}
	cur.LastRun = runAt.UTC().Format(time.RFC3339)
	if disable {
		cur.Enabled = false
	}
	s.jobs[job.ID] = cur
	return s.saveLocked()
}

type Executor struct {
	store  *Store
	ticker *time.Ticker
	stopCh chan struct{}
	doneCh chan struct{}

	runFunc RunFunc
	nowFn   func() time.Time
}

func NewExecutor(store *Store, tickInterval time.Duration, runFn RunFunc) *Executor {
	if tickInterval <= 0 {
		tickInterval = time.Second
	}
	if runFn == nil {
		runFn = func(string, string) {}
	}
	return &Executor{
		store:   store,
		runFunc: runFn,
		nowFn:   time.Now,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		ticker:  time.NewTicker(tickInterval),
	}
}

func (e *Executor) Start() {
	go func() {
		defer close(e.doneCh)
		for {
			select {
			case <-e.stopCh:
				return
			case <-e.ticker.C:
				e.check(e.nowFn().UTC())
			}
		}
	}()
}

func (e *Executor) Stop() {
	close(e.stopCh)
	e.ticker.Stop()
	<-e.doneCh
}

func (e *Executor) check(now time.Time) {
	jobs := e.store.List()
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		due, disableAfterRun, err := nextDue(job, now)
		if err != nil || !due {
			continue
		}
		e.runFunc(job.AgentID, job.Message)
		_ = e.store.updateAfterRun(job, now, disableAfterRun)
	}
}

func nextDue(job Job, now time.Time) (bool, bool, error) {
	if strings.HasPrefix(job.Schedule, "@every ") {
		raw := strings.TrimSpace(strings.TrimPrefix(job.Schedule, "@every "))
		d, err := time.ParseDuration(raw)
		if err != nil {
			return false, false, fmt.Errorf("scheduler: invalid duration %q: %w", raw, err)
		}
		if d <= 0 {
			return false, false, errors.New("scheduler: duration must be > 0")
		}
		last, err := parseLastRun(job.LastRun)
		if err != nil {
			return false, false, err
		}
		if last.IsZero() {
			return true, false, nil
		}
		return now.Sub(last) >= d, false, nil
	}

	oneShotAt, err := time.Parse(time.RFC3339, job.Schedule)
	if err != nil {
		return false, false, fmt.Errorf("scheduler: invalid schedule %q", job.Schedule)
	}
	last, err := parseLastRun(job.LastRun)
	if err != nil {
		return false, false, err
	}
	if !last.IsZero() {
		return false, true, nil
	}
	if now.Before(oneShotAt) {
		return false, false, nil
	}
	return true, true, nil
}

func parseLastRun(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("scheduler: invalid lastRun %q", raw)
	}
	return t, nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("scheduler: ensure directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-scheduler-*")
	if err != nil {
		return fmt.Errorf("scheduler: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("scheduler: write temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("scheduler: sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("scheduler: close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("scheduler: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("scheduler: rename temp file: %w", err)
	}
	return nil
}
