package internal

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"sync"
)

type UserLink struct {
	Username string `json:"username"`
	UserID   int64  `json:"user_id"`
}

func (l *UserLink) UnmarshalJSON(b []byte) error {
	var raw struct {
		Username  string `json:"username"`
		UserID    int64  `json:"user_id"`
		AEUserID  int64  `json:"ae_user_id"`
		TDSUserID int64  `json:"tds_user_id"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	l.Username = raw.Username
	l.UserID = cmp.Or(raw.UserID, raw.AEUserID, raw.TDSUserID)
	return nil
}

type Store struct {
	path  string
	mu    sync.Mutex
	links map[string]UserLink
}

func openStore(path string) (*Store, error) {
	s := &Store{path: path, links: make(map[string]UserLink)}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &s.links); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Save(discordID string, u UserLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[discordID] = u
	return s.write()
}

func (s *Store) Get(discordID string) (UserLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.links[discordID]
	if !ok {
		return UserLink{}, fmt.Errorf("not linked")
	}
	return u, nil
}

func (s *Store) All() map[string]UserLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.links)
}

func (s *Store) write() error {
	b, err := json.MarshalIndent(s.links, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}
