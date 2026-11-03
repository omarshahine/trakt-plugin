package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type RestoreRating struct {
	Type    string `json:"type"`
	TraktID string `json:"trakt_id"`
	Rating  int    `json:"rating"`
	RatedAt string `json:"rated_at"`
}
type RatingRestoreRow struct {
	RestoreRating
	State string `json:"state"`
}
type RatingRestoreResult struct {
	Account        string             `json:"account,omitempty"`
	Outcome        string             `json:"outcome"`
	Attempted      bool               `json:"attempted"`
	Verified       bool               `json:"verified"`
	HTTPStatus     int                `json:"http_status,omitempty"`
	AlreadyPresent int                `json:"already_present"`
	Rows           []RatingRestoreRow `json:"rows"`
}

func normalizeRestoreRating(x RestoreRating) (RestoreRating, error) {
	if _, err := preferenceType("ratings", x.Type, false); err != nil {
		return x, err
	}
	if _, err := listID(x.TraktID); err != nil {
		return x, err
	}
	if x.Rating < 1 || x.Rating > 10 {
		return x, fmt.Errorf("rating must be 1-10")
	}
	date, err := time.Parse(time.RFC3339Nano, x.RatedAt)
	if err != nil {
		return x, fmt.Errorf("exact rating timestamp required; no dates are fabricated")
	}
	x.RatedAt = date.UTC().Format(time.RFC3339Nano)
	return x, nil
}
func ratingKey(x RestoreRating) string { return x.Type + ":" + x.TraktID }

// ApplyMissingRatings only attempts reviewed missing additions. Trakt has no
// insert-if-absent primitive: the preflight/write gap is NOT an atomic lock.
// Unexpected updated counts or changed readback never produce success or rollback.
func (c *APIClient) ApplyMissingRatings(expected string, account RatingRestoreAccount, items []RestoreRating) (RatingRestoreResult, error) {
	r := RatingRestoreResult{Outcome: "not_attempted", Rows: []RatingRestoreRow{}}
	if expected == "" || expected != account.Slug || strings.TrimSpace(account.UUID) == "" {
		return r, fmt.Errorf("expected account slug and archive UUID are required")
	}
	if len(items) > 100 {
		return r, fmt.Errorf("at most 100 reviewed ratings may be restored per batch")
	}
	seen := map[string]bool{}
	types := []string{}
	typeSet := map[string]bool{}
	for _, x := range items {
		x, err := normalizeRestoreRating(x)
		if err != nil {
			return r, err
		}
		key := ratingKey(x)
		if seen[key] {
			return r, fmt.Errorf("duplicate rating target")
		}
		seen[key] = true
		r.Rows = append(r.Rows, RatingRestoreRow{RestoreRating: x, State: "not_attempted"})
		if !typeSet[x.Type] {
			typeSet[x.Type] = true
			types = append(types, x.Type)
		}
	}
	sort.Strings(types)
	if _, err := c.verifyRestoreAccount(expected, account); err != nil {
		return r, err
	}
	r.Account = expected
	if len(items) == 0 {
		r.Outcome, r.Verified = "noop", true
		return r, nil
	}
	before, err := c.stableRestoreRatings(expected, types)
	if err != nil {
		return r, fmt.Errorf("ratings preflight failed; no write attempted: %w", err)
	}
	missing := map[string]RestoreRating{}
	conflict := false
	for i, row := range r.Rows {
		key := ratingKey(row.RestoreRating)
		if existing, ok := before[key]; ok {
			if existing != row.RestoreRating {
				r.Rows[i].State = "conflict"
				conflict = true
			} else {
				r.Rows[i].State = "already_present"
				r.AlreadyPresent++
			}
		} else {
			r.Rows[i].State = "missing"
			missing[key] = row.RestoreRating
		}
	}
	if conflict {
		return r, fmt.Errorf("a reviewed target now has a different rating or date; entire batch refused, no overwrites")
	}
	if len(missing) == 0 {
		r.Outcome, r.Verified = "noop", true
		return r, nil
	}
	body := map[string][]any{}
	want := map[string]int{}
	for _, row := range r.Rows {
		if row.State != "missing" {
			continue
		}
		bucket := row.Type + "s"
		want[bucket]++
		body[bucket] = append(body[bucket], map[string]any{"ids": map[string]any{"trakt": json.Number(row.TraktID)}, "rating": row.Rating, "rated_at": row.RatedAt})
	}
	r.Attempted, r.Outcome = true, "unknown"
	for i := range r.Rows {
		if r.Rows[i].State == "missing" {
			r.Rows[i].State = "unknown"
		}
	}
	response, err := c.doRequest(requestParams{method: http.MethodPost, path: "/sync/ratings", body: body, auth: true, noAutoRefresh: true})
	if err != nil {
		return r, fmt.Errorf("ratings restore outcome uncertain; no automatic retry: %w", err)
	}
	defer response.Body.Close()
	r.HTTPStatus = response.StatusCode
	if response.StatusCode != http.StatusCreated {
		return r, fmt.Errorf("ratings restore returned HTTP %d; inspect current ratings before retrying", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return r, fmt.Errorf("ratings acknowledgement unreadable; outcome uncertain")
	}
	ackOK := ratingRestoreAcknowledged(data, want)
	after, err := c.stableRestoreRatings(expected, types)
	if err != nil {
		return r, fmt.Errorf("restore attempted but readback failed; no automatic retry: %w", err)
	}
	allMatch := true
	baselineMatch := len(after) == len(before)+len(missing)
	for key, previous := range before {
		if current, ok := after[key]; !ok || current != previous {
			baselineMatch = false
		}
	}
	for i, row := range r.Rows {
		current, ok := after[ratingKey(row.RestoreRating)]
		if !ok {
			r.Rows[i].State = "missing"
			allMatch = false
		} else if current != row.RestoreRating {
			r.Rows[i].State = "different"
			allMatch = false
		} else if row.State != "already_present" {
			r.Rows[i].State = "present_verified"
		}
	}
	if !ackOK || !baselineMatch || !allMatch {
		r.Outcome = "partial"
		return r, fmt.Errorf("ratings restore not fully verified (acknowledgement=%t, existing ratings unchanged=%t, targets match=%t); inspect rows, no automatic retry or rollback", ackOK, baselineMatch, allMatch)
	}
	r.Outcome, r.Verified = "verified", true
	return r, nil
}

func (c *APIClient) stableRestoreRatings(owner string, types []string) (map[string]RestoreRating, error) {
	start, err := c.GetBackupActivity()
	if err != nil {
		return nil, err
	}
	all := map[string]RestoreRating{}
	for _, kind := range types {
		rows, err := c.completeRestoreRatings(owner, kind)
		if err != nil {
			return nil, err
		}
		for key, row := range rows {
			all[key] = row
		}
	}
	end, err := c.GetBackupActivity()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(start, end) {
		return nil, fmt.Errorf("account activity changed during rating reads")
	}
	return all, nil
}

func (c *APIClient) completeRestoreRatings(owner, kind string) (map[string]RestoreRating, error) {
	result := map[string]RestoreRating{}
	path := "/users/" + url.PathEscape(owner) + "/ratings/" + kind + "s"
	err := c.walkStrictRecords(path, 1000, 100000, func(raw json.RawMessage) error {
		x, err := decodeRestoreRating(raw, kind)
		if err != nil {
			return err
		}
		key := ratingKey(x)
		if _, ok := result[key]; ok {
			return fmt.Errorf("duplicate rating media ID")
		}
		result[key] = x
		return nil
	})
	return result, err
}

func decodeRestoreRating(raw json.RawMessage, kind string) (RestoreRating, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return RestoreRating{}, fmt.Errorf("invalid rating record")
	}
	var media struct {
		IDs struct {
			Trakt json.Number `json:"trakt"`
		} `json:"ids"`
	}
	var rating int
	var date string
	if json.Unmarshal(obj[kind], &media) != nil || json.Unmarshal(obj["rating"], &rating) != nil || json.Unmarshal(obj["rated_at"], &date) != nil {
		return RestoreRating{}, fmt.Errorf("invalid rating identity/value/date")
	}
	return normalizeRestoreRating(RestoreRating{Type: kind, TraktID: media.IDs.Trakt.String(), Rating: rating, RatedAt: date})
}
