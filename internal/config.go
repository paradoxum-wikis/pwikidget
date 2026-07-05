package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type Config struct {
	DiscordToken string `json:"discord_token"`
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.DiscordToken == "" {
		return Config{}, fmt.Errorf("config must set discord_token")
	}
	return cfg, nil
}

func resolveAppID(token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://discord.com/api/v10/oauth2/applications/@me", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bot "+token)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("resolve application id: %s", res.Status)
	}
	var app struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&app); err != nil {
		return "", err
	}
	if app.ID == "" {
		return "", fmt.Errorf("resolve application id: empty response")
	}
	return app.ID, nil
}
