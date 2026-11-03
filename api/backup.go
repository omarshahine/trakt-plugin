package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GetBackupPage reads unflattened records for the scoped account backup.
// Resources are an allowlist, not arbitrary URLs. No credentials enter results.
func (c *APIClient) GetBackupPage(user, resource, listID string, p PaginationsParams) ([]json.RawMessage, Pagination, error) {
	if strings.TrimSpace(user) == "" {
		return nil, Pagination{}, fmt.Errorf("user must not be empty")
	}
	base := "/users/" + url.PathEscape(user)
	var path string
	var query map[string]string
	switch resource {
	case "history":
		path = base + "/history"
	case "watchlist":
		path = base + "/watchlist"
	case "lists":
		path = base + "/lists"
	case "ratings/movies", "ratings/shows", "ratings/seasons", "ratings/episodes":
		path = base + "/" + resource
	case "collection/movies", "collection/shows", "collection/episodes":
		path = base + "/" + resource
		query = map[string]string{"extended": "metadata"}
	case "favorites":
		path = base + "/favorites/media"
	case "playback":
		path = "/sync/playback"
	case "watched/movies", "watched/shows":
		path = base + "/" + resource
	case "hidden/calendar", "hidden/progress_watched", "hidden/progress_watched_reset", "hidden/progress_collected", "hidden/recommendations":
		path = "/users/" + resource
	case "comments/movies", "comments/shows", "comments/seasons", "comments/episodes", "comments/lists":
		path = base + "/comments/all/" + strings.TrimPrefix(resource, "comments/")
		query = map[string]string{"include_replies": "true"}
	case "notes/movies", "notes/shows", "notes/seasons", "notes/episodes", "notes/people", "notes/activities", "notes/collection_items", "notes/ratings":
		path = base + "/" + resource
	case "network/followers", "network/following", "network/friends":
		path = base + "/" + strings.TrimPrefix(resource, "network/")
	case "network/requests":
		// This endpoint is explicitly unpaginated in Trakt's contract.
		// Do not infer that arbitrary missing pagination headers mean complete.
		return c.getBackupRequests(p)
	case "likes/comments", "likes/lists":
		path = "/users/" + resource
	case "saved_filters":
		path = "/users/saved_filters"
	case "collaborations":
		path = base + "/lists/collaborations"
	case "list_items":
		id, err := strconv.ParseInt(listID, 10, 64)
		if err != nil || id <= 0 {
			return nil, Pagination{}, fmt.Errorf("backup list ID must be a positive integer")
		}
		path = base + "/lists/" + listID + "/items"
	default:
		return nil, Pagination{}, fmt.Errorf("unsupported backup resource")
	}
	return c.getRecordPageWithQuery(path, p, true, query)
}

// GetBackupDocument reads allowlisted account snapshots as raw JSON objects.
func (c *APIClient) GetBackupDocument(user, resource string) (json.RawMessage, error) {
	if strings.TrimSpace(user) == "" {
		return nil, fmt.Errorf("user must not be empty")
	}
	var path string
	var query map[string]string
	switch resource {
	case "user/settings":
		path = "/users/settings"
		query = map[string]string{"extended": "browsing,sharing"}
	case "user/profile":
		path = "/users/" + url.PathEscape(user)
		query = map[string]string{"extended": "full,vip"}
	case "user/stats":
		path = "/users/" + url.PathEscape(user) + "/stats"
	default:
		return nil, fmt.Errorf("unsupported backup document")
	}
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: path, auth: true, noCache: true, query: query})
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to read backup document: %s", r.Status)
	}
	var data json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode backup document: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || len(object) == 0 {
		return nil, fmt.Errorf("backup document must be a nonempty object")
	}
	return data, nil
}

func (c *APIClient) getBackupRequests(p PaginationsParams) ([]json.RawMessage, Pagination, error) {
	if p.Page != 1 || p.Limit < 1 || p.Limit > 1000 {
		return nil, Pagination{}, fmt.Errorf("follow requests are an unpaginated resource")
	}
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: "/users/requests", auth: true, noCache: true})
	if err != nil {
		return nil, Pagination{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, Pagination{}, fmt.Errorf("failed to read follow requests: %s", r.Status)
	}
	var rows []json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
		return nil, Pagination{}, err
	}
	if rows == nil {
		return nil, Pagination{}, fmt.Errorf("follow requests must be an array, not null")
	}
	// Unexpected pagination would make the documented full-response assumption unsafe.
	for _, key := range []string{"X-Pagination-Page", "X-Pagination-Limit", "X-Pagination-Page-Count", "X-Pagination-Item-Count"} {
		if r.Header.Get(key) != "" {
			return nil, Pagination{}, fmt.Errorf("follow requests unexpectedly paginated")
		}
	}
	limit := len(rows)
	if limit == 0 {
		limit = 1
	}
	return rows, Pagination{Page: "1", Limit: strconv.Itoa(limit), PageCount: "1", ItemCount: strconv.Itoa(len(rows))}, nil
}

// GetBackupActivity is used before and after export to detect account changes.
func (c *APIClient) GetBackupActivity() (json.RawMessage, error) {
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: "/sync/last_activities", auth: true, noCache: true})
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to read activity marker: %s", r.Status)
	}
	var marker map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&marker); err != nil {
		return nil, fmt.Errorf("decode activity marker: %w", err)
	}
	if len(marker) == 0 {
		return nil, fmt.Errorf("empty activity marker")
	}
	return json.Marshal(marker)
}
