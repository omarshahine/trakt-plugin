package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

// UpNext reads the Apple TV app's Up Next row from tv.apple.com. It covers
// every app that reports to the TV app (Apple TV+, iTunes, and partners such
// as Paramount+, Hulu, HBO Max, Peacock, Prime Video) on all of the user's
// Apple devices.
//
// Each entry is a pointer: "NextEpisode" at SxEy means SxE(y-1) finished;
// "Continue" at SxEy means SxEy is in progress. Either way SxE1..SxE(y-1)
// are reported as inferred watches. Movies in Up Next are in progress or
// unstarted, so they are never reported.
type UpNext struct {
	Cookies CookieLoader
	// Unlabeled collects show titles whose app is not named in the
	// response (HBO Max reports this way), for the HBO Max source.
	Unlabeled map[string]time.Time
}

func (u *UpNext) Name() string { return "upnext" }

const tvBase = "https://tv.apple.com"

var jwtRe = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)

func (u *UpNext) Fetch(ctx context.Context) ([]Watch, error) {
	jar, err := u.Cookies(ctx, ".tv.apple.com")
	if err != nil {
		return nil, err
	}
	if jar["media-user-token"] == "" {
		return nil, fmt.Errorf("tv.apple.com: no media-user-token cookie (sign in at tv.apple.com)")
	}

	page, err := fetch(ctx, tvBase+"/us", nil)
	if err != nil {
		return nil, err
	}
	// The token sits URL-encoded in a meta/config blob. Search the raw page
	// first; only fall back to a decoded copy (QueryUnescape fails on any
	// stray '%', so a failed decode must not hide the raw match).
	devToken := jwtRe.FindString(string(page.Body))
	if devToken == "" {
		if unescaped, err := url.QueryUnescape(string(page.Body)); err == nil {
			devToken = jwtRe.FindString(unescaped)
		} else if unescaped, err := url.PathUnescape(string(page.Body)); err == nil {
			devToken = jwtRe.FindString(unescaped)
		}
	}
	if devToken == "" {
		return nil, fmt.Errorf("tv.apple.com: web app token not found (page layout changed?)")
	}
	headers := map[string]string{
		"Accept":           "application/json",
		"Authorization":    "Bearer " + devToken,
		"media-user-token": jar["media-user-token"],
		"Cookie":           jar.Header(),
		"Origin":           tvBase,
		"Referer":          tvBase + "/",
	}

	params := url.Values{"caller": {"web"}, "locale": {"en-US"}, "pfm": {"web"}, "sf": {"143441"}, "v": {"90"}}
	cfg, err := fetch(ctx, tvBase+"/api/uts/v3/configurations?"+params.Encode(), headers)
	if err != nil {
		return nil, err
	}
	if cfg.Status != 200 {
		return nil, fmt.Errorf("tv.apple.com configurations: HTTP %d", cfg.Status)
	}
	var conf struct {
		Data struct {
			ApplicationProps struct {
				RequiredParamsMap map[string]map[string]interface{} `json:"requiredParamsMap"`
			} `json:"applicationProps"`
			UserProps struct {
				ActiveUser interface{} `json:"activeUser"`
			} `json:"userProps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(cfg.Body, &conf); err != nil {
		return nil, fmt.Errorf("tv.apple.com configurations: %w", err)
	}
	if conf.Data.UserProps.ActiveUser == nil {
		return nil, fmt.Errorf("tv.apple.com: session is not signed in")
	}
	for k, v := range conf.Data.ApplicationProps.RequiredParamsMap["Default"] {
		params.Set(k, fmt.Sprint(v))
	}

	var all []upNextItem
	channels := map[string]string{}
	next := ""
	for i := 0; i < 20; i++ {
		q := url.Values{}
		for k, v := range params {
			q[k] = v
		}
		if next != "" {
			q.Set("nextToken", next)
		}
		resp, err := fetch(ctx, tvBase+"/api/uts/v3/shelves/uts.col.UpNext?"+q.Encode(), headers)
		if err != nil {
			return nil, err
		}
		if resp.Status != 200 {
			return nil, fmt.Errorf("tv.apple.com Up Next: HTTP %d", resp.Status)
		}
		items, ch, tok, err := ParseUpNext(resp.Body)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		for k, v := range ch {
			channels[k] = v
		}
		if tok == "" {
			break
		}
		next = tok
	}

	u.Unlabeled = map[string]time.Time{}
	for _, it := range all {
		if it.Type == "Episode" && channels[it.ChannelID] == "" && it.ShowTitle != "" {
			if ts := it.When(); ts.After(u.Unlabeled[it.ShowTitle]) {
				u.Unlabeled[it.ShowTitle] = ts
			}
		}
	}
	return InferUpNext(all), nil
}

type upNextItem struct {
	Type          string `json:"type"`
	ShowTitle     string `json:"showTitle"`
	Title         string `json:"title"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	Context       string `json:"context"`
	Timestamp     int64  `json:"timestamp"`
	Playable      struct {
		ChannelID string `json:"channelId"`
	} `json:"playable"`
	ChannelID string `json:"-"`
}

// When converts the millisecond timestamp.
func (it upNextItem) When() time.Time {
	if it.Timestamp <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(it.Timestamp)
}

// ParseUpNext decodes one shelf page: items, channel id→name, next token.
func ParseUpNext(body []byte) ([]upNextItem, map[string]string, string, error) {
	var page struct {
		Data struct {
			Shelf struct {
				Items     []upNextItem `json:"items"`
				NextToken string       `json:"nextToken"`
			} `json:"shelf"`
			Channels map[string]struct {
				Name string `json:"name"`
			} `json:"channels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, nil, "", fmt.Errorf("tv.apple.com Up Next: %w", err)
	}
	ch := map[string]string{}
	for k, v := range page.Data.Channels {
		ch[k] = v.Name
	}
	items := page.Data.Shelf.Items
	for i := range items {
		items[i].ChannelID = items[i].Playable.ChannelID
	}
	return items, ch, page.Data.Shelf.NextToken, nil
}

// InferUpNext turns Up Next pointers into watched episodes: every episode
// before the pointer in the same season.
func InferUpNext(items []upNextItem) []Watch {
	var out []Watch
	for _, it := range items {
		if it.Type != "Episode" || it.ShowTitle == "" || it.SeasonNumber <= 0 || it.EpisodeNumber <= 1 {
			continue
		}
		if it.Context != "NextEpisode" && it.Context != "Continue" {
			continue
		}
		for e := 1; e < it.EpisodeNumber; e++ {
			out = append(out, Watch{
				Kind:      KindEpisode,
				Source:    "upnext",
				ShowTitle: it.ShowTitle,
				Season:    it.SeasonNumber,
				Episode:   e,
				WatchedAt: it.When(),
				Inferred:  true,
			})
		}
	}
	return out
}
