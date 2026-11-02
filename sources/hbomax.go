package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// HBOMax reads per-episode completion from the HBO Max web API. Max has no
// full history list, so it checks specific shows: its own Continue Watching
// row plus Candidates (shows seen elsewhere, e.g. unlabeled Up Next items).
// Completed episodes carry no date; they are stamped with the candidate's
// last-seen time, or left zero (the caller uses the run time).
type HBOMax struct {
	Cookies    CookieLoader
	Profile    string
	Candidates map[string]time.Time
}

func (h *HBOMax) Name() string { return "hbomax" }

const hboAPI = "https://default.prd.api.hbomax.com"

func (h *HBOMax) Fetch(ctx context.Context) ([]Watch, error) {
	jar, err := h.Cookies(ctx, ".hbomax.com")
	if err != nil {
		return nil, err
	}
	if jar["st"] == "" {
		return nil, fmt.Errorf("hbomax: no session token cookie (sign in at hbomax.com)")
	}
	dev := jar["GI_WEB_SDK_SONIC_DEVICE_ID"]
	headers := map[string]string{
		"Accept":         "application/json",
		"Cookie":         jar.Header(),
		"Origin":         "https://play.hbomax.com",
		"Referer":        "https://play.hbomax.com/",
		"x-disco-client": "WEB:10.15.7:beam:5.0.0",
		"x-disco-params": "realm=bolt,bid=beam,features=ar",
		"x-device-info":  fmt.Sprintf("beam/5.0.0 (desktop/desktop; Mac OS X/10.15.7; %s/%s)", dev, dev),
	}
	get := func(path string, v interface{}) error {
		resp, err := fetch(ctx, hboAPI+path, headers)
		if err != nil {
			return err
		}
		if resp.Status != 200 {
			return fmt.Errorf("hbomax %s: HTTP %d", strings.SplitN(path, "?", 2)[0], resp.Status)
		}
		return json.Unmarshal(resp.Body, v)
	}

	if err := h.checkProfile(get); err != nil {
		return nil, err
	}

	shows := map[string]time.Time{}
	for k, v := range h.Candidates {
		shows[k] = v
	}
	var mystuff hboDoc
	if err := get("/cms/routes/my-stuff?include=default&decorators=viewingHistory", &mystuff); err == nil {
		for _, name := range mystuff.continueWatchingShows() {
			if _, ok := shows[name]; !ok {
				shows[name] = time.Time{}
			}
		}
	}

	var out []Watch
	for name, seen := range shows {
		var search hboDoc
		if err := get("/cms/routes/search/result?include=default&contentFilter[query]="+url.QueryEscape(name), &search); err != nil {
			continue
		}
		show := search.findShow(name)
		if show == nil {
			continue
		}
		eps, err := h.completed(get, show)
		if err != nil {
			continue
		}
		for _, e := range eps {
			out = append(out, Watch{Kind: KindEpisode, Source: "hbomax", ShowTitle: show.Attributes.Name,
				Season: e[0], Episode: e[1], WatchedAt: seen})
		}
	}
	return out, nil
}

func (h *HBOMax) checkProfile(get func(string, interface{}) error) error {
	if h.Profile == "" {
		return nil
	}
	var me struct {
		Data struct {
			Attributes struct {
				Anonymous         bool   `json:"anonymous"`
				SelectedProfileID string `json:"selectedProfileId"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := get("/users/me", &me); err != nil {
		return err
	}
	if me.Data.Attributes.Anonymous {
		return fmt.Errorf("hbomax: session is not signed in")
	}
	var profiles struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				ProfileName string `json:"profileName"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := get("/users/me/profiles", &profiles); err != nil {
		return err
	}
	for _, p := range profiles.Data {
		if p.ID == me.Data.Attributes.SelectedProfileID {
			if strings.EqualFold(p.Attributes.ProfileName, h.Profile) {
				return nil
			}
			return fmt.Errorf("hbomax: session is on profile %q, not %q (switch profile in the browser)", p.Attributes.ProfileName, h.Profile)
		}
	}
	return fmt.Errorf("hbomax: selected profile not found")
}

// completed returns [season, episode] pairs marked complete on the show page.
func (h *HBOMax) completed(get func(string, interface{}) error, show *hboEntity) ([][2]int, error) {
	alt := show.Attributes.AlternateID
	if alt == "" {
		alt = show.ID
	}
	var page hboDoc
	if err := get("/cms/routes/show/"+url.PathEscape(alt)+"?include=default&decorators=viewingHistory", &page); err != nil {
		return nil, err
	}
	seen := map[[2]int]bool{}
	var out [][2]int
	add := func(doc hboDoc) {
		for _, p := range doc.completedEpisodes() {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	add(page)
	for _, c := range page.Included {
		if c.Type != "collection" || !strings.Contains(c.Attributes.Name, "episodes") || c.Attributes.Component == nil {
			continue
		}
		for _, f := range c.Attributes.Component.Filters {
			for _, opt := range f.Options {
				var season hboDoc
				q := fmt.Sprintf("/cms/collections/%s?include=default&decorators=viewingHistory&pf[show.id]=%s&pf[seasonNumber]=%s",
					url.PathEscape(c.ID), url.QueryEscape(show.ID), url.QueryEscape(fmt.Sprint(opt.Value)))
				if err := get(q, &season); err == nil {
					add(season)
				}
			}
		}
	}
	return out, nil
}

type hboEntity struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name           string `json:"name"`
		AlternateID    string `json:"alternateId"`
		VideoType      string `json:"videoType"`
		SeasonNumber   int    `json:"seasonNumber"`
		EpisodeNumber  int    `json:"episodeNumber"`
		ViewingHistory *struct {
			Completed bool `json:"completed"`
		} `json:"viewingHistory"`
		Component *struct {
			Filters []struct {
				Options []struct {
					Value interface{} `json:"value"`
				} `json:"options"`
			} `json:"filters"`
		} `json:"component"`
	} `json:"attributes"`
	Relationships map[string]struct {
		Data json.RawMessage `json:"data"`
	} `json:"relationships"`
}

type hboDoc struct {
	Included []hboEntity `json:"included"`
}

func (d hboDoc) findShow(name string) *hboEntity {
	want := Normalize(name)
	for i := range d.Included {
		e := &d.Included[i]
		if e.Type == "show" && Normalize(e.Attributes.Name) == want {
			return e
		}
	}
	return nil
}

func (d hboDoc) completedEpisodes() [][2]int {
	var out [][2]int
	for _, e := range d.Included {
		a := e.Attributes
		if e.Type != "video" || (a.VideoType != "" && a.VideoType != "EPISODE") {
			continue
		}
		if a.ViewingHistory != nil && a.ViewingHistory.Completed && a.SeasonNumber > 0 && a.EpisodeNumber > 0 {
			out = append(out, [2]int{a.SeasonNumber, a.EpisodeNumber})
		}
	}
	return out
}

// continueWatchingShows names the shows behind episodes in Continue Watching.
func (d hboDoc) continueWatchingShows() []string {
	byKey := map[string]hboEntity{}
	for _, e := range d.Included {
		byKey[e.Type+"/"+e.ID] = e
	}
	var out []string
	for _, c := range d.Included {
		if c.Type != "collection" || !strings.Contains(c.Attributes.Name, "continue-watching") {
			continue
		}
		var items []struct{ ID, Type string }
		_ = json.Unmarshal(c.Relationships["items"].Data, &items)
		for _, it := range items {
			ci := byKey[it.Type+"/"+it.ID]
			var v struct{ ID, Type string }
			if err := json.Unmarshal(ci.Relationships["video"].Data, &v); err != nil {
				continue
			}
			video := byKey[v.Type+"/"+v.ID]
			var s struct{ ID, Type string }
			if err := json.Unmarshal(video.Relationships["show"].Data, &s); err != nil {
				continue
			}
			if show, ok := byKey[s.Type+"/"+s.ID]; ok && show.Attributes.Name != "" {
				out = append(out, show.Attributes.Name)
			}
		}
	}
	return out
}
