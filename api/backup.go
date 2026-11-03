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
	switch resource {
	case "history":
		path = base + "/history"
	case "watchlist":
		path = base + "/watchlist"
	case "lists":
		path = base + "/lists"
	case "list_items":
		id, err := strconv.ParseInt(listID, 10, 64)
		if err != nil || id <= 0 {
			return nil, Pagination{}, fmt.Errorf("backup list ID must be a positive integer")
		}
		path = base + "/lists/" + listID + "/items"
	default:
		return nil, Pagination{}, fmt.Errorf("unsupported backup resource")
	}
	return c.getRecordPage(path, p, true)
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
