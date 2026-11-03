package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type PreferenceChange struct {
	Resource string
	Action   string
	Type     string
	IDs      []string
	Rating   int
	RatedAt  string
}

type PreferenceResult struct {
	Resource string `json:"resource"`
	Account  string `json:"account,omitempty"`
	ListChangeResult
}

func preferenceType(resource, kind string, allowAll bool) (string, error) {
	if resource != "ratings" && resource != "favorites" {
		return "", fmt.Errorf("resource must be ratings or favorites")
	}
	if allowAll && kind == "all" {
		if resource == "favorites" {
			return "media", nil
		}
		return "", nil
	}
	switch kind {
	case "movie", "show":
		return kind + "s", nil
	case "season", "episode":
		if resource == "ratings" {
			return kind + "s", nil
		}
	}
	return "", fmt.Errorf("unsupported media type for %s", resource)
}

// GetPreferences retains the service's raw records and pagination; one page is
// not an assertion of complete account coverage.
func (c *APIClient) GetPreferences(user, resource, kind string, p PaginationsParams) ([]json.RawMessage, Pagination, error) {
	if strings.TrimSpace(user) == "" {
		return nil, Pagination{}, fmt.Errorf("user required")
	}
	bucket, err := preferenceType(resource, kind, true)
	if err != nil {
		return nil, Pagination{}, err
	}
	path := "/users/" + url.PathEscape(user) + "/" + resource
	if bucket != "" {
		path += "/" + bucket
	}
	return c.getRecordPage(path, p, true)
}

// PreviewPreferenceChange builds only; it does not load credentials or call HTTP.
func PreviewPreferenceChange(change PreferenceChange) (ListChangePreview, error) {
	p := ListChangePreview{Action: change.Action, Method: http.MethodPost, Path: "/sync/" + change.Resource}
	bucket, err := preferenceType(change.Resource, change.Type, false)
	if err != nil {
		return p, err
	}
	validAction := change.Action == "remove" || (change.Resource == "ratings" && change.Action == "set") || (change.Resource == "favorites" && change.Action == "add")
	if !validAction {
		return p, fmt.Errorf("unsupported preference action")
	}
	if change.Resource == "ratings" && change.Action == "set" {
		if change.Rating < 1 || change.Rating > 10 {
			return p, fmt.Errorf("rating must be an integer from 1 to 10")
		}
		if change.RatedAt != "" {
			date, err := time.Parse(time.RFC3339Nano, change.RatedAt)
			if err != nil {
				return p, fmt.Errorf("rated-at must be an ISO8601 timestamp")
			}
			change.RatedAt = date.UTC().Format(time.RFC3339Nano)
		}
	} else if change.Rating != 0 || change.RatedAt != "" {
		return p, fmt.Errorf("rating and rated-at apply only to ratings set")
	}
	ids, err := listIDs(change.IDs)
	if err != nil {
		return p, err
	}
	items := make([]any, 0, len(ids))
	for _, id := range ids {
		item := map[string]any{"ids": map[string]any{"trakt": id}}
		if change.Resource == "ratings" && change.Action == "set" {
			item["rating"] = change.Rating
			if change.RatedAt != "" {
				item["rated_at"] = change.RatedAt
			}
		}
		items = append(items, item)
	}
	p.Body = map[string]any{bucket: items}
	if change.Action == "remove" {
		p.Path += "/remove"
	}
	return p, nil
}

// ApplyPreferenceChange verifies a freshly resolved account against an explicit
// expected slug before any sync write. Automatic mutation replay is disabled.
func (c *APIClient) ApplyPreferenceChange(expected string, change PreferenceChange) (PreferenceResult, error) {
	result := PreferenceResult{Resource: change.Resource, ListChangeResult: ListChangeResult{Action: change.Action, Outcome: "not_attempted"}}
	p, err := PreviewPreferenceChange(change)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(expected) == "" {
		return result, fmt.Errorf("expect-user is required for an applied preference change")
	}
	owner, err := c.ResolveListOwner()
	if err != nil {
		return result, err
	}
	if owner != expected {
		return result, fmt.Errorf("authenticated account does not match expect-user; no write attempted")
	}
	result.Account = owner
	result.Outcome = "unknown"
	r, err := c.doRequest(requestParams{method: http.MethodPost, path: p.Path, body: p.Body, auth: true, noAutoRefresh: true})
	if err != nil {
		return result, err
	}
	defer r.Body.Close()
	result.Status = r.StatusCode
	expectedStatus := http.StatusCreated
	if change.Action == "remove" {
		expectedStatus = http.StatusOK
	}
	if r.StatusCode != expectedStatus {
		return result, fmt.Errorf("Trakt %s %s returned HTTP %d; no automatic retry, inspect account before retrying (420 means account limit)", change.Resource, change.Action, r.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return result, fmt.Errorf("preference response unreadable; outcome uncertain")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return result, fmt.Errorf("invalid preference response; outcome uncertain")
	}
	result.Response = data
	var missing map[string][]json.RawMessage
	if json.Unmarshal(object["not_found"], &missing) != nil || missing == nil {
		return result, fmt.Errorf("missing not_found result; inspect account before retrying")
	}
	for _, rows := range missing {
		if len(rows) > 0 {
			result.Outcome = "partial"
			return result, fmt.Errorf("some preference items were not found; inspect result before retrying")
		}
	}
	bucket, _ := preferenceType(change.Resource, change.Type, false)
	field := "added"
	if change.Action == "remove" {
		field = "deleted"
	}
	var counts map[string]int
	if json.Unmarshal(object[field], &counts) != nil || counts == nil {
		return result, fmt.Errorf("missing preference count; inspect account before retrying")
	}
	count, ok := counts[bucket]
	if !ok || count < 0 {
		return result, fmt.Errorf("invalid preference count; inspect account before retrying")
	}
	// Some API responses report updated ratings separately; preserve and reconcile
	// them when present. Favorites can report already-existing entries.
	extra := "updated"
	if change.Resource == "favorites" {
		extra = "existing"
	}
	if change.Action != "remove" {
		if raw, ok := object[extra]; ok && string(raw) != "null" {
			var extraCounts map[string]int
			if json.Unmarshal(raw, &extraCounts) != nil || extraCounts == nil || extraCounts[bucket] < 0 {
				return result, fmt.Errorf("invalid %s counts; inspect account before retrying", extra)
			}
			count += extraCounts[bucket]
		}
	}
	if count != len(change.IDs) {
		result.Outcome = "partial"
		return result, fmt.Errorf("counts do not confirm every preference item; inspect result before retrying")
	}
	result.Outcome = "success"
	return result, nil
}
