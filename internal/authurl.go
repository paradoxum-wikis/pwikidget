package internal

import "net/url"

func widgetAuthURL(appID string) string {
	q := url.Values{
		"client_id":     {appID},
		"response_type": {"token"},
		"scope":         {"openid sdk.social_layer_presence"},
	}
	return "https://discord.com/oauth2/authorize?" + q.Encode()
}
