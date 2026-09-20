package internal

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type WikiClient struct {
	http *http.Client
	ua   string
}

func newWikiClient(ua string) *WikiClient {
	return &WikiClient{
		http: &http.Client{Timeout: 15 * time.Second},
		ua:   ua,
	}
}

type UserProfile struct {
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
	Handle  string
	Name    string
	Logo    string
	Favicon string
}

type integratedProfileCard struct {
	User      string               `json:"user"`
	MetaItems []integratedMetaItem `json:"meta_items"`
}

type integratedMetaItem struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

func (c *WikiClient) apiURL(w Wiki, params url.Values) string {
	q := url.Values{
		"action":        {"query"},
		"format":        {"json"},
		"formatversion": {"2"},
	}
	maps.Copy(q, params)
	return "https://" + w.Domain + "/api.php?" + q.Encode()
}

func (c *WikiClient) getJSON(u string, out any) error {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua)

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *WikiClient) LookupUsername(w Wiki, userID int64) (string, error) {
	var out struct {
		Query struct {
			Users []struct {
				Name string `json:"name"`
			} `json:"users"`
		} `json:"query"`
	}
	if err := c.getJSON(c.apiURL(w, url.Values{
		"list":      {"users"},
		"ususerids": {strconv.FormatInt(userID, 10)},
	}), &out); err != nil {
		return "", err
	}
	if len(out.Query.Users) == 0 || out.Query.Users[0].Name == "" {
		return "", fmt.Errorf("user %d not found on %s", userID, w.Domain)
	}
	return out.Query.Users[0].Name, nil
}

func (c *WikiClient) LookupUserID(w Wiki, username string) (int64, error) {
	var out struct {
		Query struct {
			Users []struct {
				Name    string `json:"name"`
				Missing bool   `json:"missing"`
				UserID  int64  `json:"userid"`
			} `json:"users"`
		} `json:"query"`
	}
	if err := c.getJSON(c.apiURL(w, url.Values{
		"list":    {"users"},
		"ususers": {username},
	}), &out); err != nil {
		return 0, err
	}
	if len(out.Query.Users) == 0 || out.Query.Users[0].Missing || out.Query.Users[0].UserID == 0 {
		return 0, fmt.Errorf("user %q not found on %s", username, w.Domain)
	}
	return out.Query.Users[0].UserID, nil
}

func (c *WikiClient) GetWikiInfo(w Wiki) (WikiInfo, error) {
	var out struct {
		Query struct {
			General struct {
				Sitename string `json:"sitename"`
				Logo     string `json:"logo"`
				Favicon  string `json:"favicon"`
			} `json:"general"`
			Pages []struct {
				Title     string `json:"title"`
				ImageInfo []struct {
					URL      string `json:"url"`
					ThumbURL string `json:"thumburl"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := c.getJSON(c.apiURL(w, url.Values{
		"meta":       {"siteinfo"},
		"siprop":     {"general"},
		"titles":     {"File:Site-logo.png|File:Site-favicon.ico"},
		"prop":       {"imageinfo"},
		"iiprop":     {"url"},
		"iiurlwidth": {"128"},
	}), &out); err != nil {
		return WikiInfo{}, err
	}

	info := WikiInfo{Handle: w.Handle, Name: out.Query.General.Sitename}
	for _, page := range out.Query.Pages {
		if len(page.ImageInfo) == 0 {
			continue
		}
		u := page.ImageInfo[0].ThumbURL
		if u == "" {
			u = page.ImageInfo[0].URL
		}
		switch page.Title {
		case "File:Site-logo.png":
			info.Logo = u
		case "File:Site-favicon.ico":
			info.Favicon = u
		}
	}
	if info.Logo == "" {
		info.Logo = out.Query.General.Logo
	}
	if info.Favicon == "" {
		info.Favicon = out.Query.General.Favicon
	}
	if info.Name == "" {
		info.Name = w.Handle
	}
	return info, nil
}

func (c *WikiClient) GetFandomProfile(w Wiki, userID int64) (UserProfile, error) {
	q := url.Values{
		"controller": {"UserProfile"},
		"method":     {"getUserData"},
		"format":     {"json"},
		"userId":     {strconv.FormatInt(userID, 10)},
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
			Registration string   `json:"registration"`
			Tags         []string `json:"tags"`
		} `json:"userData"`
	}
	if err := c.getJSON("https://"+w.Domain+"/wikia.php?"+q.Encode(), &out); err != nil {
		return UserProfile{}, err
	}
	if out.UserData.ID == 0 {
		return UserProfile{}, fmt.Errorf("profile not found on %s", w.Domain)
	}
	return UserProfile{
		Username:     out.UserData.Username,
		DisplayName:  out.UserData.Name,
		Avatar:       out.UserData.Avatar,
		Bio:          out.UserData.Bio,
		LocalEdits:   out.UserData.LocalEdits,
		Posts:        out.UserData.Posts,
		Registration: out.UserData.Registration,
		Tags:         out.UserData.Tags,
	}, nil
}

func (c *WikiClient) GetIntegratedProfile(w Wiki, userID int64, fallbackUsername string) (UserProfile, error) {
	username := fallbackUsername
	if name, err := c.LookupUsername(w, userID); err == nil {
		username = name
	}

	var out struct {
		Query struct {
			Users []struct {
				Name         string   `json:"name"`
				Missing      bool     `json:"missing"`
				EditCount    int      `json:"editcount"`
				Registration string   `json:"registration"`
				Groups       []string `json:"groups"`
			} `json:"users"`
			Cards []integratedProfileCard `json:"integratedprofilecard"`
		} `json:"query"`
	}
	if err := c.getJSON(c.apiURL(w, url.Values{
		"list":    {"users|integratedprofilecard"},
		"ususers": {username},
		"usprop":  {"groups|registration|editcount"},
		"ipcuser": {username},
	}), &out); err != nil {
		return UserProfile{}, err
	}
	if len(out.Query.Users) == 0 || out.Query.Users[0].Missing {
		return UserProfile{}, fmt.Errorf("user %q not found on %s", username, w.Domain)
	}

	user := out.Query.Users[0]
	profile := UserProfile{
		Username:     user.Name,
		LocalEdits:   user.EditCount,
		Registration: formatRegistration(user.Registration),
		Tags:         groupTags(user.Groups),
	}
	if len(out.Query.Cards) > 0 {
		profile.Posts = postCount(out.Query.Cards[0].MetaItems)
	}
	return profile, nil
}

func (c *WikiClient) ValidateUser(username string, userID int64) error {
	if _, err := c.GetFandomProfile(wikiAE, userID); err != nil {
		return fmt.Errorf("alter-ego: %w", err)
	}
	if _, err := c.GetIntegratedProfile(wikiTDS, userID, username); err != nil {
		return fmt.Errorf("tds: %w", err)
	}
	return nil
}

var groupLabels = map[string]string{
	"bureaucrat":        "Bureaucrat",
	"sysop":             "Administrator",
	"content-moderator": "Content moderator",
	"discuss-moderator": "Discussion moderator",
	"bot":               "Bot",
	"suppress":          "Operator",
}

func groupTags(groups []string) []string {
	tags := make([]string, 0, len(groups))
	for _, g := range groups {
		if label, ok := groupLabels[g]; ok {
			tags = append(tags, label)
		}
	}
	return tags
}

func postCount(items []integratedMetaItem) int {
	for _, item := range items {
		if item.ID == "discuss-posts" {
			n, _ := strconv.Atoi(strings.ReplaceAll(item.Value, ",", ""))
			return n
		}
	}
	return 0
}

func formatRegistration(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Format("2 January 2006")
}

func urlPathEscape(s string) string {
	return strings.ReplaceAll(s, " ", "_")
}
