package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type RestoreFavorite struct {
	Type    string `json:"type"`
	TraktID string `json:"trakt_id"`
}
type FavoriteRestoreRow struct {
	RestoreFavorite
	State string `json:"state"`
}
type FavoriteRestoreResult struct {
	Account        string               `json:"account,omitempty"`
	Outcome        string               `json:"outcome"`
	Attempted      bool                 `json:"attempted"`
	Verified       bool                 `json:"verified"`
	HTTPStatus     int                  `json:"http_status,omitempty"`
	AlreadyPresent int                  `json:"already_present"`
	Rows           []FavoriteRestoreRow `json:"rows"`
}

func normalizeRestoreFavorite(x RestoreFavorite) (RestoreFavorite, error) {
	if x.Type != "movie" && x.Type != "show" {
		return x, fmt.Errorf("favorite membership supports movie/show only")
	}
	if _, err := listID(x.TraktID); err != nil {
		return x, err
	}
	return x, nil
}
func favoriteKey(x RestoreFavorite) string { return x.Type + ":" + x.TraktID }

// FavoriteMembership intentionally ignores ranks, notes and timestamps. No
// unsupported metadata can become an instruction to restore or clear a field.
func FavoriteMembership(raw json.RawMessage) (RestoreFavorite, error) {
	var row struct {
		Type  string `json:"type"`
		Movie struct {
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		} `json:"movie"`
		Show struct {
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		} `json:"show"`
	}
	if json.Unmarshal(raw, &row) != nil {
		return RestoreFavorite{}, fmt.Errorf("invalid favorite record")
	}
	id := row.Movie.IDs.Trakt
	if row.Type == "show" {
		id = row.Show.IDs.Trakt
	}
	return normalizeRestoreFavorite(RestoreFavorite{Type: row.Type, TraktID: id.String()})
}

// ApplyMissingFavorites adds membership only. It never sends removals or rank,
// note or timestamp fields, and never replays an uncertain mutation.
func (c *APIClient) ApplyMissingFavorites(expected string, account RestoreAccount, items []RestoreFavorite) (FavoriteRestoreResult, error) {
	r := FavoriteRestoreResult{Outcome: "not_attempted", Rows: []FavoriteRestoreRow{}}
	if len(items) > 100 {
		return r, fmt.Errorf("at most 100 reviewed favorites per batch")
	}
	seen := map[string]bool{}
	for _, x := range items {
		x, err := normalizeRestoreFavorite(x)
		if err != nil {
			return r, err
		}
		key := favoriteKey(x)
		if seen[key] {
			return r, fmt.Errorf("duplicate favorite target")
		}
		seen[key] = true
		r.Rows = append(r.Rows, FavoriteRestoreRow{RestoreFavorite: x, State: "not_attempted"})
	}
	settings, err := c.verifyRestoreAccount(expected, account)
	if err != nil {
		return r, err
	}
	r.Account = expected
	if len(items) == 0 {
		r.Outcome, r.Verified = "noop", true
		return r, nil
	}
	before, err := c.stableRestoreFavorites(expected)
	if err != nil {
		return r, fmt.Errorf("favorites preflight failed; no write attempted: %w", err)
	}
	missing := map[string]RestoreFavorite{}
	for i, row := range r.Rows {
		key := favoriteKey(row.RestoreFavorite)
		if _, ok := before[key]; ok {
			r.Rows[i].State = "already_present"
			r.AlreadyPresent++
		} else {
			r.Rows[i].State = "missing"
			missing[key] = row.RestoreFavorite
		}
	}
	if len(missing) == 0 {
		r.Outcome, r.Verified = "noop", true
		return r, nil
	}
	var capacity struct {
		Limits struct {
			Favorites struct {
				ItemCount *int `json:"item_count"`
			} `json:"favorites"`
		} `json:"limits"`
	}
	if json.Unmarshal(settings, &capacity) != nil || (capacity.Limits.Favorites.ItemCount != nil && *capacity.Limits.Favorites.ItemCount < 0) {
		return r, fmt.Errorf("invalid live favorites capacity; no write attempted")
	}
	if cap := capacity.Limits.Favorites.ItemCount; cap != nil && *cap > 0 && len(before)+len(missing) > *cap {
		return r, fmt.Errorf("favorites account limit would be exceeded; entire batch refused")
	}
	body := map[string][]any{}
	want := map[string]int{}
	for _, row := range r.Rows {
		if row.State != "missing" {
			continue
		}
		bucket := row.Type + "s"
		want[bucket]++
		body[bucket] = append(body[bucket], map[string]any{"ids": map[string]any{"trakt": json.Number(row.TraktID)}})
	}
	r.Attempted, r.Outcome = true, "unknown"
	for i := range r.Rows {
		if r.Rows[i].State == "missing" {
			r.Rows[i].State = "unknown"
		}
	}
	response, err := c.doRequest(requestParams{method: http.MethodPost, path: "/sync/favorites", body: body, auth: true, noAutoRefresh: true})
	if err != nil {
		return r, fmt.Errorf("favorites restore outcome uncertain; no automatic retry: %w", err)
	}
	defer response.Body.Close()
	r.HTTPStatus = response.StatusCode
	if response.StatusCode != http.StatusCreated {
		return r, fmt.Errorf("favorites restore returned HTTP %d; inspect current membership before retrying", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return r, fmt.Errorf("favorites acknowledgement unreadable; outcome uncertain")
	}
	ackOK := restoreAddAcknowledged(data, want, true)
	after, err := c.stableRestoreFavorites(expected)
	if err != nil {
		return r, fmt.Errorf("favorites restore attempted but readback failed; no automatic retry: %w", err)
	}
	baselineMatch := len(after) == len(before)+len(missing)
	for key := range before {
		if _, ok := after[key]; !ok {
			baselineMatch = false
		}
	}
	allMatch := true
	for i, row := range r.Rows {
		if _, ok := after[favoriteKey(row.RestoreFavorite)]; !ok {
			r.Rows[i].State = "missing"
			allMatch = false
		} else if row.State != "already_present" {
			r.Rows[i].State = "present_verified"
		}
	}
	if !ackOK || !baselineMatch || !allMatch {
		r.Outcome = "partial"
		return r, fmt.Errorf("favorites restoration not fully verified (acknowledgement=%t, existing membership unchanged=%t, targets present=%t); inspect rows, no automatic retry or rollback", ackOK, baselineMatch, allMatch)
	}
	r.Outcome, r.Verified = "verified", true
	return r, nil
}

func (c *APIClient) stableRestoreFavorites(owner string) (map[string]RestoreFavorite, error) {
	start, err := c.GetBackupActivity()
	if err != nil {
		return nil, err
	}
	result := map[string]RestoreFavorite{}
	path := "/users/" + url.PathEscape(owner) + "/favorites/media"
	err = c.walkStrictRecords(path, 1000, 100000, func(raw json.RawMessage) error {
		x, err := FavoriteMembership(raw)
		if err != nil {
			return err
		}
		key := favoriteKey(x)
		if _, ok := result[key]; ok {
			return fmt.Errorf("duplicate favorite membership")
		}
		result[key] = x
		return nil
	})
	if err != nil {
		return nil, err
	}
	end, err := c.GetBackupActivity()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(start, end) {
		return nil, fmt.Errorf("account activity changed during favorites read")
	}
	return result, nil
}
