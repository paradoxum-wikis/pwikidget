package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type UserLink struct {
	DiscordID string `json:"discord_id"`
	Username  string `json:"username"`
	AEUserID  int64  `json:"ae_user_id"`
	TDSUserID int64  `json:"tds_user_id"`
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
	return s, json.Unmarshal(b, &s.links)
}

func (s *Store) Save(u UserLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[u.DiscordID] = u
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

func (s *Store) All() []UserLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]UserLink, 0, len(s.links))
	for _, u := range s.links {
		out = append(out, u)
	}
	return out
}

func (s *Store) write() error {
	b, err := json.MarshalIndent(s.links, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0644)
}
