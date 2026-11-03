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

type RestoreCollection struct {
	Type        string             `json:"type"`
	TraktID     string             `json:"trakt_id"`
	Metadata    CollectionMetadata `json:"metadata"`
	CollectedAt string             `json:"collected_at"`
}
type CollectionRestoreRow struct {
	RestoreCollection
	State string `json:"state"`
}
type CollectionRestoreResult struct {
	Account        string                 `json:"account,omitempty"`
	Outcome        string                 `json:"outcome"`
	Attempted      bool                   `json:"attempted"`
	Verified       bool                   `json:"verified"`
	HTTPStatus     int                    `json:"http_status,omitempty"`
	AlreadyPresent int                    `json:"already_present"`
	Rows           []CollectionRestoreRow `json:"rows"`
}

type CollectionMetadata struct {
	MediaType     string `json:"media_type,omitempty"`
	Resolution    string `json:"resolution,omitempty"`
	Audio         string `json:"audio,omitempty"`
	AudioChannels string `json:"audio_channels,omitempty"`
}

func NormalizeRestoreCollection(x RestoreCollection) (RestoreCollection, error) {
	if x.Type != "movie" && x.Type != "episode" {
		return x, fmt.Errorf("only flat movie/episode collection supported")
	}
	if _, err := listID(x.TraktID); err != nil {
		return x, err
	}
	date, err := time.Parse(time.RFC3339Nano, x.CollectedAt)
	if err != nil || date.IsZero() {
		return x, fmt.Errorf("known exact collection date required")
	}
	x.CollectedAt = date.UTC().Format(time.RFC3339Nano)
	for _, p := range [][2]string{{x.Metadata.MediaType, "digital"}, {x.Metadata.Resolution, "hd_1080p"}, {x.Metadata.Audio, "dts"}, {x.Metadata.AudioChannels, "5.1"}} {
		if p[0] != "" && p[0] != p[1] {
			return x, fmt.Errorf("technical value not yet supported by narrow collection executor")
		}
	}
	return x, nil
}
func normalizeRestoreCollection(x RestoreCollection) (RestoreCollection, error) {
	return NormalizeRestoreCollection(x)
}

// DecodeRestoreCollection never silently drops unsupported technical fields.
func DecodeRestoreCollection(raw json.RawMessage, kind string) (RestoreCollection, error) {
	var row map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil || row == nil {
		return RestoreCollection{}, fmt.Errorf("invalid collection row")
	}
	var media struct {
		IDs struct {
			Trakt json.Number `json:"trakt"`
		} `json:"ids"`
	}
	var date string
	if json.Unmarshal(row[kind], &media) != nil || json.Unmarshal(row["collected_at"], &date) != nil {
		return RestoreCollection{}, fmt.Errorf("invalid collection identity/date")
	}
	if v, ok := row["type"]; ok {
		var t string
		if json.Unmarshal(v, &t) != nil || t != kind {
			return RestoreCollection{}, fmt.Errorf("conflicting collection type")
		}
	}
	fields := map[string]json.RawMessage{}
	if m, ok := row["metadata"]; ok && string(m) != "null" {
		if json.Unmarshal(m, &fields) != nil || fields == nil {
			return RestoreCollection{}, fmt.Errorf("invalid collection metadata")
		}
	}
	values := map[string]string{}
	for k, v := range fields {
		switch k {
		case "media_type", "resolution", "audio", "audio_channels":
			if string(v) != "null" {
				var value string
				if json.Unmarshal(v, &value) != nil || value == "" {
					return RestoreCollection{}, fmt.Errorf("invalid technical value")
				}
				values[k] = value
			}
		case "3d", "hdr":
			if string(v) != "null" {
				return RestoreCollection{}, fmt.Errorf("non-null 3d/HDR unsupported")
			}
		default:
			return RestoreCollection{}, fmt.Errorf("unknown technical field withheld")
		}
	}
	return NormalizeRestoreCollection(RestoreCollection{Type: kind, TraktID: media.IDs.Trakt.String(), CollectedAt: date, Metadata: CollectionMetadata{values["media_type"], values["resolution"], values["audio"], values["audio_channels"]}})
}
func collectionKey(x RestoreCollection) string { return x.Type + ":" + x.TraktID }

// ApplyMissingCollections only attempts reviewed missing additions. Trakt has no
// insert-if-absent primitive: the preflight/write gap is NOT an atomic lock.
// Unexpected updated counts or changed readback never produce success or rollback.
func (c *APIClient) ApplyMissingCollections(expected string, account RestoreAccount, items []RestoreCollection) (CollectionRestoreResult, error) {
	r := CollectionRestoreResult{Outcome: "not_attempted", Rows: []CollectionRestoreRow{}}
	if expected == "" || expected != account.Slug || strings.TrimSpace(account.UUID) == "" {
		return r, fmt.Errorf("expected account slug and archive UUID are required")
	}
	if len(items) > 100 {
		return r, fmt.Errorf("at most 100 reviewed collections may be restored per batch")
	}
	seen := map[string]bool{}
	types := []string{}
	typeSet := map[string]bool{}
	for _, x := range items {
		x, err := normalizeRestoreCollection(x)
		if err != nil {
			return r, err
		}
		key := collectionKey(x)
		if seen[key] {
			return r, fmt.Errorf("duplicate collection target")
		}
		seen[key] = true
		r.Rows = append(r.Rows, CollectionRestoreRow{RestoreCollection: x, State: "not_attempted"})
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
	before, err := c.stableRestoreCollections(expected, types)
	if err != nil {
		return r, fmt.Errorf("collections preflight failed; no write attempted: %w", err)
	}
	missing := map[string]RestoreCollection{}
	conflict := false
	for i, row := range r.Rows {
		key := collectionKey(row.RestoreCollection)
		if existing, ok := before[key]; ok {
			if existing != row.RestoreCollection {
				r.Rows[i].State = "conflict"
				conflict = true
			} else {
				r.Rows[i].State = "already_present"
				r.AlreadyPresent++
			}
		} else {
			r.Rows[i].State = "missing"
			missing[key] = row.RestoreCollection
		}
	}
	if conflict {
		return r, fmt.Errorf("a reviewed target now has a different collection or date; entire batch refused, no overwrites")
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
		item := map[string]any{"ids": map[string]any{"trakt": json.Number(row.TraktID)}, "collected_at": row.CollectedAt}
		for k, v := range map[string]string{"media_type": row.Metadata.MediaType, "resolution": row.Metadata.Resolution, "audio": row.Metadata.Audio, "audio_channels": row.Metadata.AudioChannels} {
			if v != "" {
				item[k] = v
			}
		}
		body[bucket] = append(body[bucket], item)
	}
	r.Attempted, r.Outcome = true, "unknown"
	for i := range r.Rows {
		if r.Rows[i].State == "missing" {
			r.Rows[i].State = "unknown"
		}
	}
	response, err := c.doRequest(requestParams{method: http.MethodPost, path: "/sync/collection", body: body, auth: true, noAutoRefresh: true})
	if err != nil {
		return r, fmt.Errorf("collections restore outcome uncertain; no automatic retry: %w", err)
	}
	defer response.Body.Close()
	r.HTTPStatus = response.StatusCode
	if response.StatusCode != http.StatusCreated {
		return r, fmt.Errorf("collections restore returned HTTP %d; inspect current collections before retrying", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return r, fmt.Errorf("collections acknowledgement unreadable; outcome uncertain")
	}
	ackOK := restoreAddAcknowledged(data, want, false)
	after, err := c.stableRestoreCollections(expected, types)
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
		current, ok := after[collectionKey(row.RestoreCollection)]
		if !ok {
			r.Rows[i].State = "missing"
			allMatch = false
		} else if current != row.RestoreCollection {
			r.Rows[i].State = "different"
			allMatch = false
		} else if row.State != "already_present" {
			r.Rows[i].State = "present_verified"
		}
	}
	if !ackOK || !baselineMatch || !allMatch {
		r.Outcome = "partial"
		return r, fmt.Errorf("collections restore not fully verified (acknowledgement=%t, existing collections unchanged=%t, targets match=%t); inspect rows, no automatic retry or rollback", ackOK, baselineMatch, allMatch)
	}
	r.Outcome, r.Verified = "verified", true
	return r, nil
}

func (c *APIClient) stableRestoreCollections(owner string, types []string) (map[string]RestoreCollection, error) {
	start, err := c.GetBackupActivity()
	if err != nil {
		return nil, err
	}
	all := map[string]RestoreCollection{}
	for _, kind := range types {
		rows, err := c.completeRestoreCollections(owner, kind)
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
		return nil, fmt.Errorf("account activity changed during collection reads")
	}
	return all, nil
}

func (c *APIClient) completeRestoreCollections(owner, kind string) (map[string]RestoreCollection, error) {
	result := map[string]RestoreCollection{}
	path := "/users/" + url.PathEscape(owner) + "/collection/" + kind + "s"
	err := c.walkStrictRecords(path, 1000, 100000, func(raw json.RawMessage) error {
		x, err := decodeRestoreCollection(raw, kind)
		if err != nil {
			return err
		}
		key := collectionKey(x)
		if _, ok := result[key]; ok {
			return fmt.Errorf("duplicate collection media ID")
		}
		result[key] = x
		return nil
	}, map[string]string{"extended": "metadata"})
	return result, err
}

func decodeRestoreCollection(raw json.RawMessage, kind string) (RestoreCollection, error) {
	return DecodeRestoreCollection(raw, kind)
}
