package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

type ShowSub struct {
	User   string `json:"user"`
	Season int    `json:"season"`
	// Episode files the season already had when subscribing.
	Have   int  `json:"have,omitempty"`
	Future bool `json:"future,omitempty"`
}

func (a ShowSub) same(b ShowSub) bool {
	if a.User != b.User || a.Future != b.Future {
		return false
	}
	return a.Future || a.Season == b.Season
}

type data struct {
	Movies map[int][]string  `json:"movies"`
	Shows  map[int][]ShowSub `json:"shows"`
}

type Store struct {
	mu   sync.Mutex
	path string
	data data
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "notifications.json")}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, err
		}
	}
	if s.data.Movies == nil {
		s.data.Movies = map[int][]string{}
	}
	if s.data.Shows == nil {
		s.data.Shows = map[int][]ShowSub{}
	}
	return s, s.save()
}

func (s *Store) save() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) commit() {
	if err := s.save(); err != nil {
		log.Printf("saving notifications: %v", err)
	}
}

func (s *Store) AddMovie(tmdbID int, user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(s.data.Movies[tmdbID], user) {
		s.data.Movies[tmdbID] = append(s.data.Movies[tmdbID], user)
		s.commit()
	}
}

func (s *Store) HasMovie(tmdbID int, user string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.data.Movies[tmdbID], user)
}

func (s *Store) Movies() map[int][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int][]string, len(s.data.Movies))
	for id, users := range s.data.Movies {
		out[id] = slices.Clone(users)
	}
	return out
}

func (s *Store) RemoveMovie(tmdbID int, users []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	left := slices.DeleteFunc(s.data.Movies[tmdbID], func(u string) bool { return slices.Contains(users, u) })
	if len(left) == 0 {
		delete(s.data.Movies, tmdbID)
	} else {
		s.data.Movies[tmdbID] = left
	}
	s.commit()
}

func (s *Store) AddShow(tvdbID int, sub ShowSub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.ContainsFunc(s.data.Shows[tvdbID], sub.same) {
		s.data.Shows[tvdbID] = append(s.data.Shows[tvdbID], sub)
		s.commit()
	}
}

func (s *Store) HasShow(tvdbID int, sub ShowSub) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.data.Shows[tvdbID], sub.same)
}

func (s *Store) Shows() map[int][]ShowSub {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int][]ShowSub, len(s.data.Shows))
	for id, subs := range s.data.Shows {
		out[id] = slices.Clone(subs)
	}
	return out
}

func (s *Store) RemoveShow(tvdbID int, subs []ShowSub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	left := slices.DeleteFunc(s.data.Shows[tvdbID], func(have ShowSub) bool { return slices.ContainsFunc(subs, have.same) })
	if len(left) == 0 {
		delete(s.data.Shows, tvdbID)
	} else {
		s.data.Shows[tvdbID] = left
	}
	s.commit()
}
