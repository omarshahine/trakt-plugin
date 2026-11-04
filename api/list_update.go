package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

type ListUpdate struct {
	List              string
	Name              *string
	Description       *string
	Privacy           *string
	ExpectedUpdatedAt string
}
type ListUpdateResult struct {
	Action         string   `json:"action"`
	Outcome        string   `json:"outcome"`
	Attempted      bool     `json:"attempted"`
	Verified       bool     `json:"verified"`
	HTTPStatus     int      `json:"http_status,omitempty"`
	Account        string   `json:"account,omitempty"`
	ListID         string   `json:"list_id"`
	Fields         []string `json:"fields"`
	ItemsUnchanged bool     `json:"items_unchanged"`
}

func PreviewListUpdate(user string, x ListUpdate) (ListChangePreview, error) {
	p := ListChangePreview{Action: "update", Method: http.MethodPut}
	if strings.TrimSpace(user) == "" {
		return p, fmt.Errorf("user required")
	}
	if _, err := listID(x.List); err != nil {
		return p, fmt.Errorf("canonical numeric LIST ID required; use lists to resolve a slug before renaming")
	}
	body := map[string]any{}
	if x.Name != nil {
		if !utf8.ValidString(*x.Name) || strings.TrimSpace(*x.Name) == "" || utf8.RuneCountInString(*x.Name) > 255 {
			return p, fmt.Errorf("name must be nonempty valid UTF-8, at most 255 characters")
		}
		body["name"] = *x.Name
	}
	if x.Description != nil {
		if !utf8.ValidString(*x.Description) || utf8.RuneCountInString(*x.Description) > 10000 {
			return p, fmt.Errorf("description must be valid UTF-8, at most 10000 characters")
		}
		body["description"] = *x.Description
	}
	if x.Privacy != nil {
		switch *x.Privacy {
		case "private", "link", "friends", "public":
		default:
			return p, fmt.Errorf("invalid privacy")
		}
		body["privacy"] = *x.Privacy
	}
	if len(body) == 0 {
		return p, fmt.Errorf("explicitly supply at least one of name/description/privacy; empty description clears it")
	}
	p.Path = "/users/" + url.PathEscape(user) + "/lists/" + x.List
	p.Body = body
	return p, nil
}

type listEditState struct {
	Raw         map[string]json.RawMessage
	Name        string
	Description string
	Privacy     string
	Updated     string
	Count       int
	ItemsHash   string
	Items       map[string]json.RawMessage
}

func (c *APIClient) listEditMetadata(owner, id string) (listEditState, error) {
	state := listEditState{}
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: "/users/" + url.PathEscape(owner) + "/lists/" + id, auth: true, noCache: true, noAutoRefresh: true})
	if err != nil {
		return state, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return state, fmt.Errorf("list metadata read HTTP %d", r.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return state, fmt.Errorf("list metadata unreadable or too large")
	}
	if json.Unmarshal(data, &state.Raw) != nil || state.Raw == nil {
		return state, fmt.Errorf("invalid list metadata")
	}
	var ids struct {
		Trakt json.Number `json:"trakt"`
	}
	var user struct {
		IDs struct {
			Slug string `json:"slug"`
		} `json:"ids"`
	}
	var count *int
	if json.Unmarshal(state.Raw["ids"], &ids) != nil || ids.Trakt.String() != id || json.Unmarshal(state.Raw["user"], &user) != nil || user.IDs.Slug != owner {
		return state, fmt.Errorf("list ID/owner do not match; no permission fallback")
	}
	if json.Unmarshal(state.Raw["name"], &state.Name) != nil || strings.TrimSpace(state.Name) == "" || json.Unmarshal(state.Raw["privacy"], &state.Privacy) != nil || json.Unmarshal(state.Raw["updated_at"], &state.Updated) != nil || state.Updated == "" || json.Unmarshal(state.Raw["item_count"], &count) != nil || count == nil || *count < 0 {
		return state, fmt.Errorf("list consistency metadata missing")
	}
	if v, ok := state.Raw["description"]; ok && string(v) != "null" {
		if json.Unmarshal(v, &state.Description) != nil {
			return state, fmt.Errorf("invalid list description")
		}
	}
	state.Count = *count
	return state, nil
}
func (c *APIClient) stableListEditState(owner, id string) (listEditState, error) {
	before, err := c.listEditMetadata(owner, id)
	if err != nil {
		return before, err
	}
	items := map[string]json.RawMessage{}
	ranks := map[int]bool{}
	err = c.walkStrictRecords("/users/"+url.PathEscape(owner)+"/lists/"+id+"/items", 1000, 100000, func(raw json.RawMessage) error {
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil || row == nil {
			return fmt.Errorf("invalid list item")
		}
		var entry json.Number
		var kind string
		var rank *int
		if json.Unmarshal(row["id"], &entry) != nil || json.Unmarshal(row["type"], &kind) != nil || json.Unmarshal(row["rank"], &rank) != nil || rank == nil || *rank < 1 {
			return fmt.Errorf("list item identity/rank missing")
		}
		if ranks[*rank] {
			return fmt.Errorf("duplicate list rank; cannot verify order")
		}
		ranks[*rank] = true
		if _, err := listID(entry.String()); err != nil {
			return err
		}
		switch kind {
		case "movie", "show", "season", "episode", "person":
		default:
			return fmt.Errorf("unsupported list item type")
		}
		var media struct {
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		}
		if json.Unmarshal(row[kind], &media) != nil {
			return fmt.Errorf("list media missing")
		}
		if _, err := listID(media.IDs.Trakt.String()); err != nil {
			return err
		}
		if _, ok := items[entry.String()]; ok {
			return fmt.Errorf("duplicate list entry")
		}
		normalized := map[string]json.RawMessage{}
		for _, k := range []string{"id", "type", "rank", "listed_at", "notes"} {
			if value, ok := row[k]; ok {
				normalized[k] = value
			}
		}
		mediaID, _ := json.Marshal(media.IDs.Trakt.String())
		normalized["media_id"] = mediaID
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return err
		}
		items[entry.String()] = encoded
		return nil
	})
	if err != nil {
		return before, err
	}
	after, err := c.listEditMetadata(owner, id)
	if err != nil {
		return before, err
	}
	if before.Updated != after.Updated || before.Count != after.Count || len(items) != before.Count || !sameEditSettings(before, after, nil) {
		return before, fmt.Errorf("list changed while reading; no stable snapshot")
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return before, err
	}
	sum := sha256.Sum256(encoded)
	before.ItemsHash = hex.EncodeToString(sum[:])
	before.Items = items
	return before, nil
}

// Ignores only explicitly requested fields and known server-maintained counters.
// Catalog child metadata is not part of the list membership/order signature.
func sameEditSettings(a, b listEditState, changed map[string]any) bool {
	for _, k := range []string{"name", "description", "privacy", "display_numbers", "allow_comments", "sort_by", "sort_how", "created_at", "type"} {
		if _, ok := changed[k]; ok {
			continue
		}
		av, aOK := a.Raw[k]
		bv, bOK := b.Raw[k]
		if aOK != bOK || !bytes.Equal(bytes.TrimSpace(av), bytes.TrimSpace(bv)) {
			return false
		}
	}
	return true
}
func (c *APIClient) ApplyListUpdate(expected string, x ListUpdate) (ListUpdateResult, error) {
	result := ListUpdateResult{Action: "update", Outcome: "not_attempted", ListID: x.List, Fields: []string{}}
	p, err := PreviewListUpdate("me", x)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(expected) == "" {
		return result, fmt.Errorf("expect-user required before editing list metadata")
	}
	// No token refresh/persistence even on preflight reads.
	reader := *c
	reader.Credentials.RefreshToken = ""
	c = &reader
	owner, err := c.ResolveListOwner()
	if err != nil {
		return result, err
	}
	if owner != expected {
		return result, fmt.Errorf("live account does not match expect-user; no write attempted")
	}
	result.Account = owner
	before, err := c.stableListEditState(owner, x.List)
	if err != nil {
		return result, fmt.Errorf("list update preflight refused: %w", err)
	}
	if x.ExpectedUpdatedAt != "" && x.ExpectedUpdatedAt != before.Updated {
		return result, fmt.Errorf("list revision differs from expect-updated-at; no write attempted")
	}
	body := p.Body.(map[string]any)
	matches := true
	current := map[string]string{"name": before.Name, "description": before.Description, "privacy": before.Privacy}
	for k, v := range body {
		result.Fields = append(result.Fields, k)
		if current[k] != v {
			matches = false
		}
	}
	sort.Strings(result.Fields)
	if matches {
		result.Outcome = "noop"
		result.Verified = true
		result.ItemsUnchanged = true
		return result, nil
	}
	p, err = PreviewListUpdate(owner, x)
	if err != nil {
		return result, err
	}
	result.Attempted = true
	result.Outcome = "unknown"
	r, err := c.doRequest(requestParams{method: p.Method, path: p.Path, auth: true, noAutoRefresh: true, body: p.Body})
	if err != nil {
		return result, fmt.Errorf("list update uncertain; inspect before explicit follow-up: %w", err)
	}
	result.HTTPStatus = r.StatusCode
	r.Body.Close()
	if r.StatusCode != 200 {
		return result, fmt.Errorf("list update returned HTTP %d; no automatic retry", r.StatusCode)
	}
	after, err := c.stableListEditState(owner, x.List)
	if err != nil {
		return result, fmt.Errorf("update attempted but readback failed; no automatic retry: %w", err)
	}
	result.ItemsUnchanged = before.ItemsHash == after.ItemsHash && before.Count == after.Count
	actual := map[string]string{"name": after.Name, "description": after.Description, "privacy": after.Privacy}
	verified := true
	for k, v := range body {
		if actual[k] != v {
			verified = false
		}
	}
	if !verified || !result.ItemsUnchanged || !sameEditSettings(before, after, body) {
		result.Outcome = "partial"
		return result, fmt.Errorf("list update not fully verified; inspect requested/unrequested settings and item state; no automatic retry or rollback")
	}
	result.Outcome = "verified"
	result.Verified = true
	return result, nil
}
