package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// GetUserLists reads custom-list metadata, including private lists when the
// authenticated account has access. Raw records retain fields added by Trakt
// without a client release; they are deliberately not flattened to titles.
func (c *APIClient) GetUserLists(user string, params PaginationsParams) ([]json.RawMessage, Pagination, error) {
	if strings.TrimSpace(user) == "" {
		return nil, Pagination{}, fmt.Errorf("user must not be empty")
	}
	return c.getListRecords("/users/"+url.PathEscape(user)+"/lists", params)
}

// GetUserListItems accepts a list's Trakt ID or slug. Records retain rank,
// notes, timestamps, IDs and all returned item types, including seasons,
// episodes and people. Only fields actually returned by the API are available.
func (c *APIClient) GetUserListItems(user, list string, params PaginationsParams) ([]json.RawMessage, Pagination, error) {
	if strings.TrimSpace(user) == "" || strings.TrimSpace(list) == "" {
		return nil, Pagination{}, fmt.Errorf("user and list must not be empty")
	}
	return c.getListRecords("/users/"+url.PathEscape(user)+"/lists/"+url.PathEscape(list)+"/items", params)
}

func (c *APIClient) getListRecords(path string, params PaginationsParams) ([]json.RawMessage, Pagination, error) {
	return c.getRecordPage(path, params, false)
}

func (c *APIClient) getRecordPage(path string, params PaginationsParams, fresh bool) ([]json.RawMessage, Pagination, error) {
	return c.getRecordPageWithQuery(path, params, fresh, nil)
}

func (c *APIClient) getRecordPageWithQuery(path string, params PaginationsParams, fresh bool, query map[string]string) ([]json.RawMessage, Pagination, error) {
	if params.Page < 1 || params.Limit < 1 || params.Limit > 1000 {
		return nil, Pagination{}, fmt.Errorf("page must be positive and limit must be between 1 and 1000")
	}
	resp, err := c.doRequest(requestParams{method: http.MethodGet, path: path, auth: true, pagination: params, noCache: fresh, query: query})
	if err != nil {
		return nil, Pagination{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Do not echo response bodies: authentication failures can contain
		// sensitive data. Preserve status without hiding permission errors.
		return nil, Pagination{}, fmt.Errorf("failed to read Trakt records: %s", resp.Status)
	}
	var records []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, Pagination{}, fmt.Errorf("decode Trakt records: %w", err)
	}
	if records == nil {
		records = []json.RawMessage{}
	}
	return records, Pagination{
		Page:      resp.Header.Get("X-Pagination-Page"),
		Limit:     resp.Header.Get("X-Pagination-Limit"),
		PageCount: resp.Header.Get("X-Pagination-Page-Count"),
		ItemCount: resp.Header.Get("X-Pagination-Item-Count"),
	}, nil
}
