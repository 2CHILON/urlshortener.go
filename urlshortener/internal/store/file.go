package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// FileStore keeps all links in memory (fast reads) and periodically snapshots
// them to a JSON file using write-to-temp + rename, so a crash never leaves a
// half-written file. With an empty path it is purely in-memory (handy for tests).
type FileStore struct {
	mu    sync.RWMutex
	links map[string]*Link
	dirty bool
	path  string

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// Open loads existing data from path (if any) and starts the background flusher.
func Open(path string, flushEvery time.Duration) (*FileStore, error) {
	if flushEvery <= 0 {
		flushEvery = 5 * time.Second
	}
	s := &FileStore{
		links: make(map[string]*Link),
		path:  path,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	if path != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	go s.loop(flushEvery)
	return s, nil
}

func (s *FileStore) load() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	var links []Link
	if err := json.Unmarshal(data, &links); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	for i := range links {
		l := links[i]
		s.links[l.Code] = &l
	}
	return nil
}

func (s *FileStore) loop(every time.Duration) {
	defer close(s.done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			_ = s.flush() // error is retried on the next tick
		case <-s.stop:
			return
		}
	}
}

func (s *FileStore) flush() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	snapshot := make([]Link, 0, len(s.links))
	for _, l := range s.links {
		snapshot = append(snapshot, *l)
	}
	s.dirty = false
	s.mu.Unlock()

	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].CreatedAt.Before(snapshot[j].CreatedAt) })

	err := writeAtomic(s.path, snapshot)
	if err != nil {
		s.mu.Lock()
		s.dirty = true // try again next tick
		s.mu.Unlock()
	}
	return err
}

func writeAtomic(path string, links []Link) error {
	data, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *FileStore) Create(l Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[l.Code]; ok {
		return ErrExists
	}
	c := l
	s.links[l.Code] = &c
	s.dirty = true
	return nil
}

func (s *FileStore) Get(code string) (Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}
	return *l, nil
}

func (s *FileStore) RecordClick(code string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[code]
	if !ok {
		return ErrNotFound
	}
	l.Clicks++
	t := at
	l.LastAccess = &t
	s.dirty = true
	return nil
}

func (s *FileStore) Delete(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[code]; !ok {
		return ErrNotFound
	}
	delete(s.links, code)
	s.dirty = true
	return nil
}

func (s *FileStore) DeleteExpiredBefore(cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for code, l := range s.links {
		if l.ExpiresAt != nil && l.ExpiresAt.Before(cutoff) {
			delete(s.links, code)
			n++
		}
	}
	if n > 0 {
		s.dirty = true
	}
	return n
}

// Close stops the flusher and performs a final flush.
func (s *FileStore) Close() error {
	var err error
	s.once.Do(func() {
		close(s.stop)
		<-s.done
		err = s.flush()
	})
	return err
}