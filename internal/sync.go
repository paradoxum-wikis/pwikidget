package internal

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var ErrOAuthRequired = errors.New("oauth required")

type dynamicField struct {
	Type  int    `json:"type"`
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type widgetPayload struct {
	Username string `json:"username"`
	Data     struct {
		Dynamic []dynamicField `json:"dynamic"`
	} `json:"data"`
}

const autoSyncInterval = 30 * time.Minute

func (b *Bot) startAutoSync() {
	go func() {
		t := time.NewTicker(autoSyncInterval)
		defer t.Stop()
		b.syncAll()
		for range t.C {
			b.syncAll()
		}
	}()
}

func (b *Bot) syncAll() {
	links := b.store.All()
	if len(links) == 0 {
		return
	}
	aeInfo, err := b.fandom.GetWikiInfo(wikiAE)
	if err != nil {
		log.Printf("auto sync wiki %s: %v", wikiAE, err)
		return
	}
	tdsInfo, err := b.fandom.GetWikiInfo(wikiTDS)
	if err != nil {
		log.Printf("auto sync wiki %s: %v", wikiTDS, err)
		return
	}
	for discordID, link := range links {
		if err := b.syncUserWithInfo(discordID, link, aeInfo, tdsInfo); err != nil {
			log.Printf("auto sync %s: %v", discordID, err)
		}
	}
}

func (b *Bot) syncUser(discordID string, link UserLink) error {
	aeInfo, err := b.fandom.GetWikiInfo(wikiAE)
	if err != nil {
		return err
	}
	tdsInfo, err := b.fandom.GetWikiInfo(wikiTDS)
	if err != nil {
		return err
	}
	return b.syncUserWithInfo(discordID, link, aeInfo, tdsInfo)
}

func (b *Bot) syncUserWithInfo(discordID string, link UserLink, aeInfo, tdsInfo WikiInfo) error {
	aeProfile, err := b.fandom.GetProfile(wikiAE, link.UserID)
	if err != nil {
		return err
	}
	tdsProfile, err := b.fandom.GetProfile(wikiTDS, link.UserID)
	if err != nil {
		return err
	}
	return syncWidget(b.appID, b.token, discordID, link.Username, aeInfo, aeProfile, tdsInfo, tdsProfile)
}

func syncWidget(appID, token, discordID, username string, aeInfo WikiInfo, ae UserProfile, tdsInfo WikiInfo, tds UserProfile) error {
	var fields []dynamicField
	fields = appendWikiFields(fields, prefixAE, aeInfo, ae, true)
	fields = appendWikiFields(fields, prefixTDS, tdsInfo, tds, false)
	fields = append(fields,
		dynamicField{Type: 1, Name: "total_edits", Value: formatWithCommas(ae.LocalEdits + tds.LocalEdits)},
		dynamicField{Type: 2, Name: "total_edit_count", Value: ae.LocalEdits + tds.LocalEdits},
		dynamicField{Type: 1, Name: "total_posts", Value: formatWithCommas(ae.Posts + tds.Posts)},
		dynamicField{Type: 2, Name: "total_post_count", Value: ae.Posts + tds.Posts},
	)

	var payload widgetPayload
	payload.Username = username
	payload.Data.Dynamic = fields

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://discord.com/api/v9/applications/%s/users/%s/identities/%s/profile", appID, discordID, discordID)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/discord/discord-api-docs, 1.0.0)")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode == 400 {
			log.Printf("discord sync payload fields: %s", describeDynamicFields(fields))
			log.Printf("discord sync error body: %s", body)
		}
		return fmt.Errorf("discord sync %s: %w", res.Status, parseDiscordAPIError(body))
	}
	return nil
}

func appendWikiFields(fields []dynamicField, prefix string, wiki WikiInfo, p UserProfile, includeShared bool) []dynamicField {
	if includeShared {
		fields = append(fields,
			dynamicField{Type: 1, Name: prefix + "display_name", Value: cmp.Or(p.DisplayName, p.Username)},
			dynamicField{Type: 1, Name: prefix + "wiki_username", Value: p.Username},
		)
		if p.Avatar != "" {
			fields = append(fields, dynamicField{
				Type:  3,
				Name:  prefix + "avatar",
				Value: map[string]string{"url": webpURL(p.Avatar)},
			})
		}
	}

	tags := strings.Join(p.Tags, ", ")
	if tags == "" {
		tags = "Editor"
	}

	fields = append(fields,
		dynamicField{Type: 1, Name: prefix + "wiki", Value: "@" + wiki.Subdomain},
		dynamicField{Type: 1, Name: prefix + "wiki_name", Value: wiki.Name},
		dynamicField{Type: 1, Name: prefix + "edits", Value: formatWithCommas(p.LocalEdits)},
		dynamicField{Type: 2, Name: prefix + "edit_count", Value: p.LocalEdits},
		dynamicField{Type: 2, Name: prefix + "edit_goal", Value: editGoal(p.LocalEdits)},
		dynamicField{Type: 1, Name: prefix + "posts", Value: formatWithCommas(p.Posts)},
		dynamicField{Type: 2, Name: prefix + "post_count", Value: p.Posts},
		dynamicField{Type: 1, Name: prefix + "registered", Value: p.Registration},
		dynamicField{Type: 1, Name: prefix + "tags", Value: tags},
	)
	if includeShared && p.Bio != "" {
		fields = append(fields, dynamicField{Type: 1, Name: prefix + "bio", Value: truncate(p.Bio, 100)})
	}
	if wiki.Logo != "" {
		fields = append(fields, dynamicField{
			Type:  3,
			Name:  prefix + "wiki_logo",
			Value: map[string]string{"url": webpURL(wiki.Logo)},
		})
	}
	if wiki.Favicon != "" {
		fields = append(fields, dynamicField{
			Type:  3,
			Name:  prefix + "wiki_favicon",
			Value: map[string]string{"url": webpURL(wiki.Favicon)},
		})
	}
	return fields
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func webpURL(u string) string {
	if u == "" {
		return ""
	}
	if strings.Contains(u, "?") {
		return u + "&format=webp"
	}
	return u + "?format=webp"
}

func editGoal(edits int) int {
	for _, g := range []int{50, 100, 250, 500, 1000, 2500, 5000} {
		if edits < g {
			return g
		}
	}
	return (edits/5000 + 1) * 5000
}

func formatWithCommas(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	start := len(s) % 3
	if start == 0 {
		start = 3
	}
	b.WriteString(s[:start])
	for i := start; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

type discordAPIError struct {
	Message string          `json:"message"`
	Code    int             `json:"code"`
	Errors  json.RawMessage `json:"errors"`
}

func describeDynamicFields(fields []dynamicField) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("%s(type=%d)", f.Name, f.Type)
	}
	return strings.Join(parts, ", ")
}

func parseDiscordAPIError(body []byte) error {
	var api discordAPIError
	if json.Unmarshal(body, &api) != nil || api.Message == "" {
		return fmt.Errorf("%s", body)
	}
	detail := api.Message
	if len(api.Errors) > 0 {
		detail += ": " + string(api.Errors)
	}
	switch api.Code {
	case 20012:
		return fmt.Errorf("%s (code %d): bot token does not belong to this application", api.Message, api.Code)
	case 50001, 50026:
		return fmt.Errorf("%s (code %d): %w", api.Message, api.Code, ErrOAuthRequired)
	default:
		return fmt.Errorf("%s (code %d)", detail, api.Code)
	}
}
