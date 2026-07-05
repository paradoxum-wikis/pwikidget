package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cgt.name/pkg/go-mwclient"
)

type FandomClient struct {
	http *http.Client
	ua   string
}

func newFandomClient(ua string) *FandomClient {
	return &FandomClient{
		http: &http.Client{Timeout: 15 * time.Second},
		ua:   ua,
	}
}

type UserProfile struct {
	ID           int64
	Username     string
	DisplayName  string
	Avatar       string
	Bio          string
	LocalEdits   int
	Posts        int
	Registration string
	Tags         []string
}

type WikiInfo struct {
	Subdomain string
	Name      string
	Logo      string
	Favicon   string
}

func (c *FandomClient) wikiBase(sub string) string {
	return "https://" + sub + ".fandom.com"
}

func (c *FandomClient) LookupUserID(sub, username string) (int64, error) {
	client, err := mwclient.New(c.wikiBase(sub)+"/api.php", c.ua)
	if err != nil {
		return 0, err
	}
	resp, err := client.Get(map[string]string{
		"action":   "query",
		"list":     "users",
		"ususers":  username,
		"format":   "json",
		"continue": "",
	})
	if err != nil {
		return 0, err
	}
	users, err := resp.GetObjectArray("query", "users")
	if err != nil || len(users) == 0 {
		return 0, fmt.Errorf("user %q not found on %s", username, sub)
	}
	if missing, _ := users[0].GetString("missing"); missing != "" {
		return 0, fmt.Errorf("user %q not found on %s", username, sub)
	}
	id, err := users[0].GetInt64("userid")
	if err != nil {
		return 0, fmt.Errorf("user %q not found on %s", username, sub)
	}
	return id, nil
}

func (c *FandomClient) GetWikiInfo(sub string) (WikiInfo, error) {
	u, err := url.Parse(c.wikiBase(sub) + "/api.php")
	if err != nil {
		return WikiInfo{}, err
	}
	q := u.Query()
	q.Set("action", "query")
	q.Set("meta", "siteinfo")
	q.Set("siprop", "general")
	q.Set("titles", "File:Site-logo.png|File:Site-favicon.ico")
	q.Set("prop", "imageinfo")
	q.Set("iiprop", "url")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return WikiInfo{}, err
	}
	req.Header.Set("User-Agent", c.ua)

	res, err := c.http.Do(req)
	if err != nil {
		return WikiInfo{}, err
	}
	defer res.Body.Close()

	var out struct {
		Query struct {
			General struct {
				Sitename string `json:"sitename"`
				Logo     string `json:"logo"`
				Favicon  string `json:"favicon"`
			} `json:"general"`
			Pages map[string]struct {
				Title     string `json:"title"`
				ImageInfo []struct {
					URL string `json:"url"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return WikiInfo{}, err
	}

	info := WikiInfo{Subdomain: sub, Name: out.Query.General.Sitename}
	for _, page := range out.Query.Pages {
		if len(page.ImageInfo) == 0 {
			continue
		}
		switch page.Title {
		case "File:Site-logo.png":
			info.Logo = page.ImageInfo[0].URL
		case "File:Site-favicon.ico":
			info.Favicon = page.ImageInfo[0].URL
		}
	}
	if info.Logo == "" {
		info.Logo = out.Query.General.Logo
	}
	if info.Favicon == "" {
		info.Favicon = out.Query.General.Favicon
	}
	if info.Name == "" {
		info.Name = sub
	}
	return info, nil
}

func (c *FandomClient) GetProfile(sub string, userID int64) (UserProfile, error) {
	u, err := url.Parse(c.wikiBase(sub) + "/wikia.php")
	if err != nil {
		return UserProfile{}, err
	}
	q := u.Query()
	q.Set("controller", "UserProfile")
	q.Set("method", "getUserData")
	q.Set("format", "json")
	q.Set("userId", fmt.Sprint(userID))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return UserProfile{}, err
	}
	req.Header.Set("User-Agent", c.ua)

	res, err := c.http.Do(req)
	if err != nil {
		return UserProfile{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return UserProfile{}, fmt.Errorf("%s profile request failed: %s", sub, res.Status)
	}

	var out struct {
		UserData struct {
			ID           int64    `json:"id"`
			Username     string   `json:"username"`
			Avatar       string   `json:"avatar"`
			Name         string   `json:"name"`
			Bio          string   `json:"bio"`
			LocalEdits   int      `json:"localEdits"`
			Posts        int      `json:"posts"`
			Registration any      `json:"registration"`
			Tags         []string `json:"tags"`
		} `json:"userData"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return UserProfile{}, err
	}
	if out.UserData.ID == 0 {
		return UserProfile{}, fmt.Errorf("profile not found on %s", sub)
	}
	reg, _ := out.UserData.Registration.(string)
	return UserProfile{
		ID:           out.UserData.ID,
		Username:     out.UserData.Username,
		DisplayName:  out.UserData.Name,
		Avatar:       out.UserData.Avatar,
		Bio:          out.UserData.Bio,
		LocalEdits:   out.UserData.LocalEdits,
		Posts:        out.UserData.Posts,
		Registration: reg,
		Tags:         out.UserData.Tags,
	}, nil
}

func (c *FandomClient) ResolveOnWikis(username string, userID int64) (aeID, tdsID int64, err error) {
	if userID != 0 {
		if _, err := c.GetProfile(wikiAE, userID); err != nil {
			return 0, 0, fmt.Errorf("alter-ego: %w", err)
		}
		if _, err := c.GetProfile(wikiTDS, userID); err != nil {
			return 0, 0, fmt.Errorf("tds: %w", err)
		}
		return userID, userID, nil
	}
	aeID, err = c.LookupUserID(wikiAE, username)
	if err != nil {
		return 0, 0, err
	}
	tdsID, err = c.LookupUserID(wikiTDS, username)
	if err != nil {
		return 0, 0, err
	}
	return aeID, tdsID, nil
}

func urlPathEscape(s string) string {
	return strings.ReplaceAll(s, " ", "_")
}
