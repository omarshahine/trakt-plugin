package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type DiscoveryOptions struct {
	Mode  string
	Type  string
	ID    string
	Page  int
	Limit int
}
type DiscoveryItem struct {
	Type     string          `json:"type"`
	TraktID  string          `json:"trakt_id"`
	Title    string          `json:"title"`
	Year     *int            `json:"year"`
	Watchers *int64          `json:"watchers,omitempty"`
	Media    json.RawMessage `json:"media"`
}
type DiscoveryResult struct {
	Mode  string          `json:"mode"`
	Type  string          `json:"type"`
	Items []DiscoveryItem `json:"items"`
	Pagination
	PaginationAvailable bool `json:"pagination_available"`
	WatchedFiltered     bool `json:"watched_filtered"`
}

func ValidateDiscovery(o DiscoveryOptions) error {
	if o.Type != "movie" && o.Type != "show" {
		return fmt.Errorf("type must be movie or show")
	}
	if o.Page < 1 || o.Limit < 1 || o.Limit > 100 {
		return fmt.Errorf("page must be positive; limit must be 1-100")
	}
	switch o.Mode {
	case "trending", "popular", "recommendations":
		if o.ID != "" {
			return fmt.Errorf("id only applies to related discovery")
		}
	case "related":
		if _, err := listID(o.ID); err != nil {
			return fmt.Errorf("related requires an exact positive Trakt media ID")
		}
	default:
		return fmt.Errorf("mode must be trending, popular, recommendations or related")
	}
	if o.Mode == "recommendations" && o.Page != 1 {
		return fmt.Errorf("recommendations supports limit, not arbitrary pages")
	}
	return nil
}

// Discover reads one response in service ranking order. It does not exclude
// watched titles or invent an explanation of a personalized recommendation.
func (c *APIClient) Discover(o DiscoveryOptions) (DiscoveryResult, error) {
	result := DiscoveryResult{Mode: o.Mode, Type: o.Type, Items: []DiscoveryItem{}}
	if err := ValidateDiscovery(o); err != nil {
		return result, err
	}
	bucket := o.Type + "s"
	path := "/" + bucket + "/" + o.Mode
	if o.Mode == "related" {
		path = "/" + bucket + "/" + o.ID + "/related"
	}
	if o.Mode == "recommendations" {
		path = "/recommendations/" + bucket
	}
	paging := PaginationsParams{Page: o.Page, Limit: o.Limit}
	if o.Mode == "recommendations" {
		paging.Page = 0
	}
	response, err := c.doRequest(requestParams{method: http.MethodGet, path: path, auth: true, noCache: true, noAutoRefresh: true, pagination: paging, query: map[string]string{"extended": "full"}})
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("discovery read returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return result, fmt.Errorf("discovery response unreadable or too large")
	}
	var rows []json.RawMessage
	if json.Unmarshal(data, &rows) != nil || rows == nil {
		return result, fmt.Errorf("discovery response must be an array, not null")
	}
	seen := map[string]bool{}
	for _, raw := range rows {
		media := raw
		var watchers *int64
		if o.Mode == "trending" {
			var row map[string]json.RawMessage
			if json.Unmarshal(raw, &row) != nil {
				return result, fmt.Errorf("invalid trending row")
			}
			media = row[o.Type]
			if w, ok := row["watchers"]; ok {
				if json.Unmarshal(w, &watchers) != nil || watchers == nil || *watchers < 0 {
					return result, fmt.Errorf("invalid trending watcher count")
				}
			}
		}
		var title struct {
			Title string `json:"title"`
			Year  *int   `json:"year"`
			IDs   struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		}
		if json.Unmarshal(media, &title) != nil || title.Title == "" {
			return result, fmt.Errorf("invalid discovery media record")
		}
		id := title.IDs.Trakt.String()
		if _, err := listID(id); err != nil {
			return result, fmt.Errorf("invalid discovery media ID")
		}
		if seen[id] {
			return result, fmt.Errorf("duplicate discovery media ID")
		}
		seen[id] = true
		result.Items = append(result.Items, DiscoveryItem{Type: o.Type, TraktID: id, Title: title.Title, Year: title.Year, Watchers: watchers, Media: media})
	}
	result.Pagination = Pagination{Page: response.Header.Get("X-Pagination-Page"), PageCount: response.Header.Get("X-Pagination-Page-Count"), Limit: response.Header.Get("X-Pagination-Limit"), ItemCount: response.Header.Get("X-Pagination-Item-Count")}
	result.PaginationAvailable = result.Page != "" && result.PageCount != "" && result.Pagination.Limit != "" && result.ItemCount != ""
	return result, nil
}
