package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HistoryEventRef identifies one dated play, never a media-wide deletion.
// IDs remain strings at the CLI/plugin boundary to avoid JavaScript rounding.
type HistoryEventRef struct {
	HistoryID string `json:"history_id"`
	Type      string `json:"type"`
	TraktID   string `json:"trakt_id"`
	WatchedAt string `json:"watched_at"`
	Action    string `json:"action,omitempty"`
}

type HistoryEventRemovalPreview struct {
	Action  string          `json:"action"`
	Target  HistoryEventRef `json:"target"`
	Method  string          `json:"method"`
	Path    string          `json:"path"`
	Body    any             `json:"body"`
	Applied bool            `json:"applied"`
}

type HistoryEventRemovalResult struct {
	Target      HistoryEventRef `json:"target"`
	Account     string          `json:"account,omitempty"`
	Outcome     string          `json:"outcome"`
	Attempted   bool            `json:"attempted"`
	Verified    bool            `json:"verified"`
	HTTPStatus  int             `json:"http_status,omitempty"`
	BeforeCount int             `json:"before_count"`
	AfterCount  int             `json:"after_count"`
}

func normalizeHistoryEvent(target HistoryEventRef) (HistoryEventRef, error) {
	if target.Type != "movie" && target.Type != "episode" {
		return target, fmt.Errorf("type must be movie or episode; shows/seasons are not individual watches")
	}
	for _, value := range []string{target.HistoryID, target.TraktID} {
		if _, err := listID(value); err != nil {
			return target, err
		}
	}
	date, err := time.Parse(time.RFC3339Nano, target.WatchedAt)
	if err != nil {
		return target, fmt.Errorf("watched-at must be an exact ISO8601 timestamp; unknown dates are not supported")
	}
	target.WatchedAt = date.UTC().Format(time.RFC3339Nano)
	return target, nil
}

// PreviewHistoryEventRemoval is entirely offline. The only POST selector is the
// history EVENT ID; media ID/type/date are verification guards, not selectors.
func PreviewHistoryEventRemoval(target HistoryEventRef) (HistoryEventRemovalPreview, error) {
	target, err := normalizeHistoryEvent(target)
	if err != nil {
		return HistoryEventRemovalPreview{}, err
	}
	return HistoryEventRemovalPreview{Action: "remove_event", Target: target, Method: http.MethodPost,
		Path: "/sync/history/remove", Body: map[string]any{"ids": []json.Number{json.Number(target.HistoryID)}}}, nil
}

// ApplyHistoryEventRemoval never falls back to a media selector or retries a
// mutation. Success requires a matching preflight and a complete scoped readback.
func (c *APIClient) ApplyHistoryEventRemoval(expected string, target HistoryEventRef) (HistoryEventRemovalResult, error) {
	result := HistoryEventRemovalResult{Target: target, Outcome: "not_attempted"}
	preview, err := PreviewHistoryEventRemoval(target)
	if err != nil {
		return result, err
	}
	target, result.Target = preview.Target, preview.Target
	if strings.TrimSpace(expected) == "" {
		return result, fmt.Errorf("expect-user is required with apply")
	}
	owner, err := c.ResolveListOwner()
	if err != nil {
		return result, err
	}
	if owner != expected {
		return result, fmt.Errorf("authenticated account does not match expect-user; no write attempted")
	}
	result.Account = owner
	before, err := c.stableHistoryEvents(owner, target)
	if err != nil {
		return result, fmt.Errorf("history preflight failed; no write attempted: %w", err)
	}
	result.BeforeCount = len(before)
	event, ok := before[target.HistoryID]
	if !ok {
		return result, fmt.Errorf("history event is absent from the selected media; no write attempted")
	}
	if event.Type != target.Type || event.TraktID != target.TraktID || event.WatchedAt != target.WatchedAt {
		return result, fmt.Errorf("history event no longer matches the reviewed type/media/date; no write attempted")
	}
	result.Attempted, result.Outcome = true, "unknown"
	r, err := c.doRequest(requestParams{method: preview.Method, path: preview.Path, body: preview.Body, auth: true, noAutoRefresh: true})
	if err != nil {
		return result, fmt.Errorf("history removal outcome uncertain; do not automatically retry: %w", err)
	}
	defer r.Body.Close()
	result.HTTPStatus = r.StatusCode
	if r.StatusCode != http.StatusOK {
		return result, fmt.Errorf("history removal returned HTTP %d; inspect account before retrying", r.StatusCode)
	}
	var response RemoveHistoryResp
	data, readErr := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if readErr != nil || len(data) > 1<<20 || json.Unmarshal(data, &response) != nil {
		return result, fmt.Errorf("invalid history removal response; outcome uncertain, no automatic retry")
	}
	wantMovies, wantEpisodes := 0, 0
	if target.Type == "movie" {
		wantMovies = 1
	} else {
		wantEpisodes = 1
	}
	if response.Deleted.Movies != wantMovies || response.Deleted.Episodes != wantEpisodes ||
		len(response.NotFound.Ids)+len(response.NotFound.Movies)+len(response.NotFound.Shows)+len(response.NotFound.Seasons)+len(response.NotFound.Episodes) != 0 {
		return result, fmt.Errorf("unexpected history removal counts; outcome uncertain, no automatic retry")
	}
	after, err := c.stableHistoryEvents(owner, target)
	if err != nil {
		return result, fmt.Errorf("removal attempted but readback failed; no automatic retry: %w", err)
	}
	result.AfterCount = len(after)
	if _, exists := after[target.HistoryID]; exists {
		return result, fmt.Errorf("removed event is still present in readback; no automatic retry")
	}
	if len(after) != len(before)-1 {
		return result, fmt.Errorf("other watches changed during removal; readback could not verify a single deletion")
	}
	for id, previous := range before {
		if id == target.HistoryID {
			continue
		}
		if current, exists := after[id]; !exists || current != previous {
			return result, fmt.Errorf("another watch changed or disappeared; removal cannot be verified")
		}
	}
	result.Outcome, result.Verified = "verified", true
	return result, nil
}

// Activity markers bound each paginated read, not a transactional lock. Any
// concurrent change or incomplete pagination fails closed; no mutation replay.
func (c *APIClient) stableHistoryEvents(owner string, target HistoryEventRef) (map[string]HistoryEventRef, error) {
	stamp, err := c.GetBackupActivity()
	if err != nil {
		return nil, err
	}
	events, err := c.historyEvents(owner, target)
	if err != nil {
		return nil, err
	}
	end, err := c.GetBackupActivity()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(stamp, end) {
		return nil, fmt.Errorf("account activity changed during history read")
	}
	return events, nil
}

func (c *APIClient) historyEvents(owner string, target HistoryEventRef) (map[string]HistoryEventRef, error) {
	path := "/users/" + url.PathEscape(owner) + "/history/" + target.Type + "s/" + target.TraktID
	result := map[string]HistoryEventRef{}
	pages, limit, total := 0, 0, 0
	for page := 1; page <= 100; page++ {
		rows, p, err := c.historyEventPage(path, page)
		if err != nil {
			return nil, err
		}
		values := []int{}
		for _, s := range []string{p.Page, p.PageCount, p.Limit, p.ItemCount} {
			n, e := strconv.Atoi(s)
			if e != nil {
				return nil, fmt.Errorf("missing or invalid history pagination")
			}
			values = append(values, n)
		}
		// Trakt returns page=1, page_count=0, item_count=0 and [] after
		// removing the last play. Admit only that exact empty first-page case.
		if page == 1 && values[0] == 1 && values[1] == 0 && values[2] >= 1 && values[2] <= 1000 && values[3] == 0 && len(rows) == 0 {
			return result, nil
		}
		if values[0] != page || values[1] < 1 || values[1] > 100 || values[2] < 1 || values[2] > 1000 || values[3] < 0 || values[3] > 10000 {
			return nil, fmt.Errorf("unsafe history pagination or scope exceeds 10000 events")
		}
		if page == 1 {
			pages, limit, total = values[1], values[2], values[3]
		} else if pages != values[1] || limit != values[2] || total != values[3] {
			return nil, fmt.Errorf("history pagination changed during read")
		}
		wantPages := (total + limit - 1) / limit
		if wantPages == 0 {
			wantPages = 1
		}
		wantRows := limit
		if page == pages {
			wantRows = total - limit*(pages-1)
		}
		if pages != wantPages || len(rows) != wantRows {
			return nil, fmt.Errorf("history page size/count mismatch")
		}
		for _, row := range rows {
			event, err := decodeHistoryEvent(row, target.Type)
			if err != nil || event.Type != target.Type || event.TraktID != target.TraktID {
				return nil, fmt.Errorf("invalid or mismatched history event in scoped read")
			}
			if _, exists := result[event.HistoryID]; exists {
				return nil, fmt.Errorf("duplicate history event ID during pagination")
			}
			result[event.HistoryID] = event
		}
		if page == pages {
			if len(result) != total {
				return nil, fmt.Errorf("incomplete history read")
			}
			return result, nil
		}
	}
	return nil, fmt.Errorf("history page limit exceeded")
}

func (c *APIClient) historyEventPage(path string, page int) ([]json.RawMessage, Pagination, error) {
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: path, auth: true, noCache: true, pagination: PaginationsParams{Page: page, Limit: 100}})
	if err != nil {
		return nil, Pagination{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, Pagination{}, fmt.Errorf("history read returned HTTP %d", r.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, Pagination{}, fmt.Errorf("history page unreadable or too large")
	}
	var rows []json.RawMessage
	if json.Unmarshal(data, &rows) != nil || rows == nil {
		return nil, Pagination{}, fmt.Errorf("history page must be a JSON array, not null")
	}
	return rows, Pagination{Page: r.Header.Get("X-Pagination-Page"), Limit: r.Header.Get("X-Pagination-Limit"), PageCount: r.Header.Get("X-Pagination-Page-Count"), ItemCount: r.Header.Get("X-Pagination-Item-Count")}, nil
}

func decodeHistoryEvent(raw json.RawMessage, kind string) (HistoryEventRef, error) {
	var row struct {
		ID        json.Number `json:"id"`
		Type      string      `json:"type"`
		WatchedAt string      `json:"watched_at"`
		Action    string      `json:"action"`
		Movie     struct {
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		} `json:"movie"`
		Episode struct {
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		} `json:"episode"`
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		return HistoryEventRef{}, err
	}
	id := row.Movie.IDs.Trakt
	if kind == "episode" {
		id = row.Episode.IDs.Trakt
	}
	return normalizeHistoryEvent(HistoryEventRef{HistoryID: row.ID.String(), Type: row.Type, TraktID: id.String(), WatchedAt: row.WatchedAt, Action: row.Action})
}
