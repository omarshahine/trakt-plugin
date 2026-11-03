package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type TitleBrowseOptions struct {
	Mode    string
	Type    string
	ID      string
	Season  *int
	Episode *int
}
type TitleBrowseItem struct {
	Type      string          `json:"type"`
	TraktID   string          `json:"trakt_id,omitempty"`
	Season    *int            `json:"season,omitempty"`
	Number    *int            `json:"number,omitempty"`
	AirStatus string          `json:"air_status,omitempty"`
	Media     json.RawMessage `json:"media"`
}
type TitleBrowseResult struct {
	Kind    string            `json:"kind"`
	ID      string            `json:"requested_trakt_id"`
	AsOf    string            `json:"as_of"`
	Items   []TitleBrowseItem `json:"items"`
	Season  *int              `json:"requested_season,omitempty"`
	Episode *int              `json:"requested_episode,omitempty"`
}

func ValidateTitleBrowse(o TitleBrowseOptions) error {
	if _, err := listID(o.ID); err != nil {
		return fmt.Errorf("exact positive Trakt movie/show media ID required")
	}
	switch o.Mode {
	case "details":
		if o.Type != "movie" && o.Type != "show" {
			return fmt.Errorf("type must be movie or show")
		}
		if o.Season != nil || o.Episode != nil {
			return fmt.Errorf("title details do not accept season/episode")
		}
	case "seasons":
		if o.Type != "show" || o.Season != nil || o.Episode != nil {
			return fmt.Errorf("season listing requires show only")
		}
	case "episodes", "episode":
		if o.Type != "show" || o.Season == nil || *o.Season < 0 || *o.Season > 10000 {
			return fmt.Errorf("season must be 0-10000; 0 includes specials")
		}
		if o.Mode == "episodes" && o.Episode != nil {
			return fmt.Errorf("episode list does not accept episode number")
		}
		if o.Mode == "episode" && (o.Episode == nil || *o.Episode < 0 || *o.Episode > 10000) {
			return fmt.Errorf("episode must be 0-10000")
		}
	default:
		return fmt.Errorf("unsupported title browse mode")
	}
	return nil
}

// BrowseTitle performs a single metadata GET. first_aired is broadcast metadata,
// not the user's watched state or a promise about regional streaming availability.
func (c *APIClient) BrowseTitle(o TitleBrowseOptions) (TitleBrowseResult, error) {
	return c.browseTitleAt(o, time.Now().UTC())
}
func (c *APIClient) browseTitleAt(o TitleBrowseOptions, now time.Time) (TitleBrowseResult, error) {
	r := TitleBrowseResult{Kind: o.Mode, ID: o.ID, Season: o.Season, Episode: o.Episode, AsOf: now.UTC().Format(time.RFC3339Nano), Items: []TitleBrowseItem{}}
	if err := ValidateTitleBrowse(o); err != nil {
		return r, err
	}
	path := "/" + o.Type + "s/" + o.ID
	if o.Mode != "details" {
		path = "/shows/" + o.ID + "/seasons"
	}
	if o.Season != nil {
		path += "/" + strconv.Itoa(*o.Season)
	}
	if o.Episode != nil {
		path += "/episodes/" + strconv.Itoa(*o.Episode)
	}
	resp, err := c.doRequest(requestParams{method: http.MethodGet, path: path, auth: true, noCache: true, noAutoRefresh: true, query: map[string]string{"extended": "full"}})
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("title browsing returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return r, fmt.Errorf("title metadata unreadable or too large")
	}
	rows := []json.RawMessage{}
	if o.Mode == "details" || o.Mode == "episode" {
		rows = append(rows, json.RawMessage(data))
	} else {
		if json.Unmarshal(data, &rows) != nil || rows == nil {
			return r, fmt.Errorf("season/episode listing must be an array, not null")
		}
	}
	if len(rows) > 5000 {
		return r, fmt.Errorf("title metadata exceeds 5000-record bound")
	}
	seenIDs, seenNumbers := map[string]bool{}, map[int]bool{}
	for _, raw := range rows {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return r, fmt.Errorf("invalid metadata object")
		}
		item := TitleBrowseItem{Media: raw}
		var ids struct {
			Trakt json.Number `json:"trakt"`
		}
		if v, ok := fields["ids"]; ok {
			if json.Unmarshal(v, &ids) != nil {
				return r, fmt.Errorf("invalid metadata IDs")
			}
		}
		item.TraktID = ids.Trakt.String()
		if item.TraktID != "" {
			if _, err := listID(item.TraktID); err != nil {
				return r, err
			}
			if seenIDs[item.TraktID] {
				return r, fmt.Errorf("duplicate metadata media ID")
			}
			seenIDs[item.TraktID] = true
		}
		if o.Mode == "details" {
			var title string
			if json.Unmarshal(fields["title"], &title) != nil || title == "" || item.TraktID != o.ID {
				return r, fmt.Errorf("title identity does not match request")
			}
			item.Type = o.Type
		} else {
			var number *int
			if json.Unmarshal(fields["number"], &number) != nil || number == nil || *number < 0 {
				return r, fmt.Errorf("missing/invalid season or episode number")
			}
			if seenNumbers[*number] {
				return r, fmt.Errorf("duplicate season/episode number")
			}
			seenNumbers[*number] = true
			item.Number = number
			if o.Mode == "seasons" {
				item.Type = "season"
			} else {
				item.Type = "episode"
				var season *int
				if json.Unmarshal(fields["season"], &season) != nil || season == nil || *season != *o.Season || item.TraktID == "" {
					return r, fmt.Errorf("episode identity/season does not match request")
				}
				item.Season = season
				if o.Episode != nil && *number != *o.Episode {
					return r, fmt.Errorf("episode number does not match request")
				}
				item.AirStatus = episodeAirStatus(fields["first_aired"], now)
			}
		}
		r.Items = append(r.Items, item)
	}
	return r, nil
}
func episodeAirStatus(raw json.RawMessage, now time.Time) string {
	var date string
	if json.Unmarshal(raw, &date) != nil || date == "" {
		return "unknown"
	}
	aired, err := time.Parse(time.RFC3339Nano, date)
	if err != nil || aired.IsZero() {
		return "unknown"
	}
	if aired.After(now) {
		return "upcoming"
	}
	return "aired"
}
