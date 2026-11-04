package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

type EpisodeQueueOptions struct {
	Count           int  `json:"count"`
	MaxShows        int  `json:"max_shows"`
	RuntimeMax      *int `json:"runtime_max,omitempty"`
	IncludeSpecials bool `json:"include_specials"`
}
type EpisodeQueueItem struct {
	ShowID     string          `json:"show_trakt_id"`
	ShowTitle  string          `json:"show_title"`
	Episode    TitleBrowseItem `json:"episode"`
	FactualFit []string        `json:"factual_fit"`
}
type EpisodeQueueSkip struct {
	ShowID string `json:"show_trakt_id"`
	Reason string `json:"reason"`
}
type EpisodeQueueResult struct {
	Kind           string              `json:"kind"`
	AsOf           string              `json:"as_of"`
	Account        string              `json:"account,omitempty"`
	ScanSucceeded  bool                `json:"scan_succeeded"`
	CountMet       bool                `json:"count_met"`
	StopReason     string              `json:"stop_reason"`
	ShowsAvailable int                 `json:"shows_available"`
	ShowsChecked   int                 `json:"shows_checked"`
	Options        EpisodeQueueOptions `json:"options"`
	Items          []EpisodeQueueItem  `json:"items"`
	Skipped        []EpisodeQueueSkip  `json:"skipped"`
}
type queueShow struct {
	ID, Title string
	Last      time.Time
}

func ValidateEpisodeQueue(o EpisodeQueueOptions) error {
	if o.Count < 1 || o.Count > 20 || o.MaxShows < 1 || o.MaxShows > 20 {
		return fmt.Errorf("count and max-shows must be 1-20")
	}
	if o.RuntimeMax != nil && (*o.RuntimeMax < 1 || *o.RuntimeMax > 10000) {
		return fmt.Errorf("runtime-max must be 1-10000 minutes")
	}
	return nil
}

// Only already-started shows are considered, ordered by most recent episode
// watch time, then exact string show ID. No watchlist-only or rewatch expansion.
func (c *APIClient) BuildEpisodeQueue(o EpisodeQueueOptions) (EpisodeQueueResult, error) {
	return c.buildEpisodeQueueAt(o, time.Now().UTC())
}
func (c *APIClient) buildEpisodeQueueAt(o EpisodeQueueOptions, now time.Time) (EpisodeQueueResult, error) {
	r := EpisodeQueueResult{Kind: "episode_tonight", AsOf: now.UTC().Format(time.RFC3339Nano), Options: o, Items: []EpisodeQueueItem{}, Skipped: []EpisodeQueueSkip{}, StopReason: "prepare_failed"}
	if err := ValidateEpisodeQueue(o); err != nil {
		return r, err
	}
	reader := *c
	reader.Credentials.RefreshToken = ""
	c = &reader
	owner, err := c.ResolveListOwner()
	if err != nil {
		return r, err
	}
	r.Account = owner
	stamp, err := c.GetBackupActivity()
	if err != nil {
		return r, err
	}
	shows := map[string]queueShow{}
	events := map[string]bool{}
	watched := map[string]bool{}
	err = c.walkStrictRecords("/users/"+url.PathEscape(owner)+"/history/episodes", 1000, 100000, func(raw json.RawMessage) error {
		var row struct {
			ID      json.Number `json:"id"`
			Type    string      `json:"type"`
			Watched string      `json:"watched_at"`
			Show    struct {
				Title string `json:"title"`
				IDs   struct {
					Trakt json.Number `json:"trakt"`
				} `json:"ids"`
			} `json:"show"`
			Episode struct {
				Season, Number *int
				IDs            struct {
					Trakt json.Number `json:"trakt"`
				} `json:"ids"`
			} `json:"episode"`
		}
		if json.Unmarshal(raw, &row) != nil || row.Type != "episode" || row.Show.Title == "" || row.Episode.Season == nil || row.Episode.Number == nil || *row.Episode.Season < 0 || *row.Episode.Number < 0 {
			return fmt.Errorf("invalid episode history identity")
		}
		for _, id := range []string{row.ID.String(), row.Show.IDs.Trakt.String(), row.Episode.IDs.Trakt.String()} {
			if _, err := listID(id); err != nil {
				return err
			}
		}
		if events[row.ID.String()] {
			return fmt.Errorf("duplicate history event")
		}
		events[row.ID.String()] = true
		date, err := time.Parse(time.RFC3339Nano, row.Watched)
		if err != nil || date.IsZero() {
			return fmt.Errorf("invalid episode watch date")
		}
		id := row.Show.IDs.Trakt.String()
		watched[row.Episode.IDs.Trakt.String()] = true
		if !o.IncludeSpecials && *row.Episode.Season == 0 {
			return nil
		}
		if old, ok := shows[id]; !ok || date.After(old.Last) {
			shows[id] = queueShow{id, row.Show.Title, date}
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	ordered := []queueShow{}
	for _, s := range shows {
		ordered = append(ordered, s)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Last.Equal(ordered[j].Last) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Last.After(ordered[j].Last)
	})
	r.ShowsAvailable = len(ordered)
	r.StopReason = "candidate_window_exhausted"
	for _, s := range ordered {
		if r.ShowsChecked == o.MaxShows {
			r.StopReason = "show_budget"
			break
		}
		r.ShowsChecked++
		episode, reason, err := c.queueNextEpisode(s.ID, o, watched, now)
		if err != nil {
			r.StopReason = "request_failed"
			return r, err
		}
		if reason != "" {
			r.Skipped = append(r.Skipped, EpisodeQueueSkip{s.ID, reason})
			continue
		}
		facts := []string{"Show has an episode in checked history; ordered by latest watch time", "Trakt progress marks the returned next episode incomplete", "Episode ID absent from checked history snapshot", "Broadcast first_aired is at or before as_of; not a streaming-availability claim"}
		if o.RuntimeMax != nil {
			facts = append(facts, "Returned episode runtime meets requested per-episode maximum; not a total-session budget")
		}
		r.Items = append(r.Items, EpisodeQueueItem{s.ID, s.Title, episode, facts})
		if len(r.Items) == o.Count {
			r.CountMet = true
			r.StopReason = "target_met"
			break
		}
	}
	end, err := c.GetBackupActivity()
	if err != nil {
		r.StopReason = "activity_unverified"
		return r, err
	}
	if !bytes.Equal(stamp, end) {
		r.StopReason = "activity_changed"
		return r, fmt.Errorf("account activity changed during episode scan; retained items are unverified partial results")
	}
	r.ScanSucceeded = true
	return r, nil
}

func (c *APIClient) queueNextEpisode(show string, o EpisodeQueueOptions, watched map[string]bool, now time.Time) (TitleBrowseItem, string, error) {
	zero := TitleBrowseItem{}
	resp, err := c.doRequest(requestParams{method: http.MethodGet, path: "/shows/" + show + "/progress/watched", auth: true, noCache: true, noAutoRefresh: true, query: map[string]string{"hidden": "false", "specials": strconv.FormatBool(o.IncludeSpecials), "count_specials": strconv.FormatBool(o.IncludeSpecials)}})
	if err != nil {
		return zero, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return zero, "", fmt.Errorf("episode progress returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return zero, "", fmt.Errorf("episode progress unreadable or too large")
	}
	var p struct {
		Aired, Completed *int
		Next             json.RawMessage `json:"next_episode"`
		Reset            json.RawMessage `json:"reset_at"`
		Seasons          []struct {
			Number   *int
			Episodes []struct {
				Number    *int
				Completed *bool
			}
		}
	}
	if json.Unmarshal(data, &p) != nil || p.Aired == nil || p.Completed == nil || *p.Aired < 0 || *p.Completed < 0 {
		return zero, "", fmt.Errorf("invalid progress counts")
	}
	if len(p.Reset) > 0 && string(p.Reset) != "null" {
		return zero, "reset_progress_unsupported", nil
	}
	if *p.Completed == 0 {
		return zero, "not_started_in_progress", nil
	}
	if *p.Completed >= *p.Aired && *p.Aired > 0 {
		return zero, "caught_up", nil
	}
	if len(p.Next) == 0 || string(p.Next) == "null" {
		return zero, "no_next_episode", nil
	}
	var next struct {
		Season, Number *int
		IDs            struct {
			Trakt json.Number `json:"trakt"`
		} `json:"ids"`
	}
	if json.Unmarshal(p.Next, &next) != nil || next.Season == nil || next.Number == nil || *next.Season < 0 || *next.Number < 0 {
		return zero, "", fmt.Errorf("invalid next episode identity")
	}
	if _, err := listID(next.IDs.Trakt.String()); err != nil {
		return zero, "", err
	}
	if *next.Season == 0 && !o.IncludeSpecials {
		return zero, "specials_excluded", nil
	}
	if watched[next.IDs.Trakt.String()] {
		return zero, "history_progress_disagree", nil
	}
	found := 0
	for _, s := range p.Seasons {
		if s.Number == nil {
			return zero, "", fmt.Errorf("missing progress season")
		}
		if *s.Number != *next.Season {
			continue
		}
		for _, ep := range s.Episodes {
			if ep.Number == nil {
				return zero, "", fmt.Errorf("missing progress episode number")
			}
			if *ep.Number == *next.Number {
				found++
				if ep.Completed == nil || *ep.Completed {
					return zero, "next_completion_unverified", nil
				}
			}
		}
	}
	if found != 1 {
		return zero, "next_completion_unverified", nil
	}
	b, err := c.browseTitleAt(TitleBrowseOptions{Mode: "episode", Type: "show", ID: show, Season: next.Season, Episode: next.Number}, now)
	if err != nil {
		return zero, "", err
	}
	item := b.Items[0]
	if item.TraktID != next.IDs.Trakt.String() {
		return zero, "", fmt.Errorf("progress and episode metadata IDs differ")
	}
	if item.AirStatus != "aired" {
		return zero, "next_air_" + item.AirStatus, nil
	}
	if o.RuntimeMax != nil {
		var m struct{ Runtime *int }
		if json.Unmarshal(item.Media, &m) != nil || m.Runtime == nil || *m.Runtime < 1 {
			return zero, "runtime_unknown", nil
		}
		if *m.Runtime > *o.RuntimeMax {
			return zero, "runtime_exceeds_max", nil
		}
	}
	return item, "", nil
}
