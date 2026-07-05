package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type AphonosLink struct {
	DiscordUserID  string `json:"discordUserId"`
	FandomUsername string `json:"fandomUsername"`
	FandomUserID   int64  `json:"fandomUserId"`
}

type AphonosClient struct {
	http *http.Client
}

func newAphonosClient() *AphonosClient {
	return &AphonosClient{http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *AphonosClient) Lookup(discordID string) (*AphonosLink, error) {
	req, err := http.NewRequest(http.MethodGet, "https://altershaper.t7ru.link/linked_accounts.json", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aphonos accounts: %s", res.Status)
	}
	var accounts []AphonosLink
	if err := json.NewDecoder(res.Body).Decode(&accounts); err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.DiscordUserID == discordID {
			return &a, nil
		}
	}
	return nil, nil
}
