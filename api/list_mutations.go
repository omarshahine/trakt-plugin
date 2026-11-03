package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ListChange uses Trakt media IDs for membership, and list-entry IDs for notes
// and ordering. These ID namespaces must not be interchanged.
type ListChange struct {
	Action      string
	List        string
	Name        string
	Description string
	Privacy     string
	Type        string
	IDs         []string
	Item        string
	Notes       *string
	Rank        []string
}

type ListChangePreview struct {
	Action  string `json:"action"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Body    any    `json:"body"`
	Applied bool   `json:"applied"`
}

type ListChangeResult struct {
	Action   string          `json:"action"`
	Outcome  string          `json:"outcome"`
	Status   int             `json:"http_status"`
	Response json.RawMessage `json:"response,omitempty"`
}

// ResolveListOwner reads the token's current account rather than trusting a
// cached slug from a previous login. This does not persist credential changes.
func (c *APIClient) ResolveListOwner() (string, error) {
	settings, err := c.GetBackupDocument("me", "user/settings")
	if err != nil {
		return "", err
	}
	var account struct {
		User struct {
			IDs struct {
				Slug string `json:"slug"`
			} `json:"ids"`
		} `json:"user"`
	}
	if json.Unmarshal(settings, &account) != nil || strings.TrimSpace(account.User.IDs.Slug) == "" {
		return "", fmt.Errorf("could not verify authenticated list owner")
	}
	return account.User.IDs.Slug, nil
}

func listID(value string) (json.Number, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != value {
		return "", fmt.Errorf("IDs must be canonical positive Trakt integers")
	}
	return json.Number(value), nil
}

func listIDs(values []string) ([]json.Number, error) {
	if len(values) == 0 || len(values) > 10000 {
		return nil, fmt.Errorf("supply 1-10000 IDs")
	}
	ids := make([]json.Number, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		id, err := listID(value)
		if err != nil {
			return nil, err
		}
		if seen[value] {
			return nil, fmt.Errorf("duplicate IDs")
		}
		seen[value] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// PreviewListChange validates and builds the exact mutation without credentials
// or HTTP calls. Use user="me" for an unresolved authenticated-account preview.
func PreviewListChange(user string, change ListChange) (ListChangePreview, error) {
	p := ListChangePreview{Action: change.Action, Method: http.MethodPost}
	if strings.TrimSpace(user) == "" {
		return p, fmt.Errorf("user required")
	}
	p.Path = "/users/" + url.PathEscape(user) + "/lists"
	if change.Action != "create" {
		if strings.TrimSpace(change.List) == "" {
			return p, fmt.Errorf("list ID or slug required")
		}
		p.Path += "/" + url.PathEscape(change.List)
	}
	switch change.Action {
	case "create":
		if strings.TrimSpace(change.Name) == "" {
			return p, fmt.Errorf("nonempty list name required")
		}
		privacy := change.Privacy
		if privacy == "" {
			privacy = "private"
		}
		switch privacy {
		case "private", "link", "friends", "public":
		default:
			return p, fmt.Errorf("invalid list privacy")
		}
		p.Body = map[string]any{"name": change.Name, "description": change.Description, "privacy": privacy}
	case "add", "remove":
		var bucket string
		switch change.Type {
		case "movie":
			bucket = "movies"
		case "show":
			bucket = "shows"
		case "season":
			bucket = "seasons"
		case "episode":
			bucket = "episodes"
		default:
			return p, fmt.Errorf("type must be movie, show, season or episode")
		}
		ids, err := listIDs(change.IDs)
		if err != nil {
			return p, err
		}
		items := make([]any, 0, len(ids))
		for _, id := range ids {
			items = append(items, map[string]any{"ids": map[string]any{"trakt": id}})
		}
		p.Body = map[string]any{bucket: items}
		p.Path += "/items"
		if change.Action == "remove" {
			p.Path += "/remove"
		}
	case "notes":
		if _, err := listID(change.Item); err != nil {
			return p, err
		}
		if change.Notes == nil || !utf8.ValidString(*change.Notes) || utf8.RuneCountInString(*change.Notes) > 500 {
			return p, fmt.Errorf("notes must be explicitly supplied, valid UTF-8 and at most 500 characters; empty clears them")
		}
		p.Method = http.MethodPut
		p.Path += "/items/" + change.Item
		p.Body = map[string]any{"notes": *change.Notes}
	case "reorder":
		ids, err := listIDs(change.Rank)
		if err != nil {
			return p, err
		}
		p.Path += "/items/reorder"
		p.Body = map[string]any{"rank": ids}
	default:
		return p, fmt.Errorf("unsupported list action")
	}
	return p, nil
}

// ApplyListChange is intentionally separate from preview. Mutations disable
// automatic refresh/replay: an uncertain response must never cause a duplicate
// create/add automatically. Callers must explicitly opt in before invoking it.
func (c *APIClient) ApplyListChange(user string, change ListChange) (ListChangeResult, error) {
	result := ListChangeResult{Action: change.Action, Outcome: "not_attempted"}
	p, err := PreviewListChange(user, change)
	if err != nil {
		return result, err
	}
	if change.Action == "notes" {
		settings, err := c.GetBackupDocument(user, "user/settings")
		if err != nil {
			return result, err
		}
		var account struct {
			User struct {
				VIP bool `json:"vip"`
			} `json:"user"`
		}
		if json.Unmarshal(settings, &account) != nil || !account.User.VIP {
			return result, fmt.Errorf("list-item notes require VIP; no write attempted")
		}
	}
	if change.Action == "reorder" {
		if err := c.checkListOrder(user, change); err != nil {
			return result, err
		}
	}
	result.Outcome = "unknown"
	r, err := c.doRequest(requestParams{method: p.Method, path: p.Path, body: p.Body, auth: true, noAutoRefresh: true})
	if err != nil {
		return result, err
	}
	defer r.Body.Close()
	result.Status = r.StatusCode
	expected := http.StatusOK
	switch change.Action {
	case "create", "add":
		expected = http.StatusCreated
	case "notes":
		expected = http.StatusNoContent
	}
	if r.StatusCode != expected {
		return result, fmt.Errorf("Trakt list %s returned HTTP %d; no automatic retry (420 means account limit, 403 may mean permission/VIP)", change.Action, r.StatusCode)
	}
	if expected == http.StatusNoContent {
		result.Outcome = "success"
		return result, nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return result, fmt.Errorf("mutation response unreadable; outcome uncertain, inspect list before retrying")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return result, fmt.Errorf("invalid mutation response; outcome uncertain, inspect list before retrying")
	}
	result.Response = data
	if change.Action == "create" {
		var ids struct {
			Trakt json.Number `json:"trakt"`
		}
		if json.Unmarshal(object["ids"], &ids) != nil {
			return result, fmt.Errorf("created-list ID missing; inspect lists before retrying")
		}
		if _, err := listID(ids.Trakt.String()); err != nil {
			return result, fmt.Errorf("created-list ID invalid; inspect lists before retrying")
		}
	} else if change.Action == "reorder" {
		var updated *int
		var skipped []json.RawMessage
		if json.Unmarshal(object["updated"], &updated) != nil || updated == nil || json.Unmarshal(object["skipped_ids"], &skipped) != nil || skipped == nil {
			return result, fmt.Errorf("invalid reorder result; inspect list before retrying")
		}
		if len(skipped) > 0 || *updated != len(change.Rank) {
			result.Outcome = "partial"
			return result, fmt.Errorf("reorder incomplete; inspect updated/skipped_ids, do not automatically retry")
		}
	} else {
		bucket := change.Type + "s"
		var counts map[string]int
		var missing map[string][]json.RawMessage
		field := "added"
		if change.Action == "remove" {
			field = "deleted"
		}
		if json.Unmarshal(object[field], &counts) != nil || counts == nil || json.Unmarshal(object["not_found"], &missing) != nil || missing == nil {
			return result, fmt.Errorf("invalid membership result; inspect list before retrying")
		}
		count, ok := counts[bucket]
		if !ok || count < 0 {
			return result, fmt.Errorf("missing membership count; inspect list before retrying")
		}
		if change.Action == "add" {
			var existing map[string]int
			if json.Unmarshal(object["existing"], &existing) != nil || existing == nil || existing[bucket] < 0 {
				return result, fmt.Errorf("invalid existing count; inspect list before retrying")
			}
			count += existing[bucket]
		}
		for _, items := range missing {
			if len(items) > 0 {
				result.Outcome = "partial"
				return result, fmt.Errorf("some items were not found; inspect result before retrying")
			}
		}
		if count != len(change.IDs) {
			result.Outcome = "partial"
			return result, fmt.Errorf("membership counts do not confirm every requested item; inspect result before retrying")
		}
	}
	result.Outcome = "success"
	return result, nil
}

func (c *APIClient) listOrderState(user, list string) (string, int, error) {
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: "/users/" + url.PathEscape(user) + "/lists/" + url.PathEscape(list), auth: true, noCache: true})
	if err != nil {
		return "", 0, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("read list state: HTTP %d", r.StatusCode)
	}
	var state struct {
		Updated string `json:"updated_at"`
		Count   *int   `json:"item_count"`
	}
	if json.NewDecoder(r.Body).Decode(&state) != nil || state.Updated == "" || state.Count == nil || *state.Count < 0 {
		return "", 0, fmt.Errorf("missing list consistency metadata")
	}
	return state.Updated, *state.Count, nil
}

// Reorder requires the full current permutation of list-entry IDs. Metadata and
// pagination are checked around the read, but Trakt offers no transactional lock
// between that final check and the mutation.
func (c *APIClient) checkListOrder(user string, change ListChange) error {
	stamp, count, err := c.listOrderState(user, change.List)
	if err != nil {
		return err
	}
	if count != len(change.Rank) {
		return fmt.Errorf("rank must contain every current list-entry ID exactly once")
	}
	wanted := map[string]bool{}
	for _, id := range change.Rank {
		wanted[id] = true
	}
	seen := map[string]bool{}
	pages, limit := 0, 0
	for page := 1; page <= 10000; page++ {
		rows, p, err := c.getRecordPage("/users/"+url.PathEscape(user)+"/lists/"+url.PathEscape(change.List)+"/items", PaginationsParams{Page: page, Limit: 1000}, true)
		if err != nil {
			return err
		}
		nums := []int{}
		for _, value := range []string{p.Page, p.PageCount, p.Limit, p.ItemCount} {
			n, e := strconv.Atoi(value)
			if e != nil {
				return fmt.Errorf("invalid pagination during reorder preflight")
			}
			nums = append(nums, n)
		}
		if nums[0] != page || nums[1] < 1 || nums[1] > 10000 || nums[2] < 1 || nums[3] != count {
			return fmt.Errorf("inconsistent reorder pagination")
		}
		if page == 1 {
			pages, limit = nums[1], nums[2]
		} else if nums[1] != pages || nums[2] != limit {
			return fmt.Errorf("list changed during reorder preflight")
		}
		for _, row := range rows {
			var item struct {
				ID json.Number `json:"id"`
			}
			if json.Unmarshal(row, &item) != nil {
				return fmt.Errorf("invalid list entry")
			}
			id, err := listID(item.ID.String())
			if err != nil || seen[id.String()] || !wanted[id.String()] {
				return fmt.Errorf("rank does not match current list-entry IDs")
			}
			seen[id.String()] = true
		}
		if page == pages {
			break
		}
	}
	if len(seen) != count {
		return fmt.Errorf("incomplete list read; refusing reorder")
	}
	endStamp, endCount, err := c.listOrderState(user, change.List)
	if err != nil {
		return err
	}
	if endStamp != stamp || endCount != count {
		return fmt.Errorf("list changed during reorder preflight")
	}
	return nil
}
