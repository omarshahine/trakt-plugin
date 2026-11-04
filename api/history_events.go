package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	result := map[string]HistoryEventRef{}
	path := "/users/" + url.PathEscape(owner) + "/history/" + target.Type + "s/" + target.TraktID
	err := c.walkStrictRecords(path, 100, 10000, func(raw json.RawMessage) error {
		event, err := decodeHistoryEvent(raw, target.Type)
		if err != nil || event.Type != target.Type || event.TraktID != target.TraktID {
			return fmt.Errorf("invalid or mismatched history event in scoped read")
		}
		if _, exists := result[event.HistoryID]; exists {
			return fmt.Errorf("duplicate history event ID during pagination")
		}
		result[event.HistoryID] = event
		return nil
	})
	return result, err
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
