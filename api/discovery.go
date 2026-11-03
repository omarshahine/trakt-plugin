package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type DiscoveryOptions struct {
	Mode               string
	Type               string
	ID                 string
	Page               int
	Limit              int
	ExcludeWatched     bool
	ExcludeWatchlisted bool
	ExcludeIDs         []string
	Shortlist          string
	Criteria           DiscoveryCriteria
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
	PaginationAvailable   bool               `json:"pagination_available"`
	WatchedFiltered       bool               `json:"watched_filtered"`
	WatchlistFiltered     bool               `json:"watchlist_filtered"`
	SourceCount           int                `json:"source_count"`
	DuplicatesRemoved     int                `json:"duplicates_removed"`
	ExcludedWatched       int                `json:"excluded_watched"`
	ExcludedWatchlisted   int                `json:"excluded_watchlisted"`
	ExcludedIDs           int                `json:"excluded_ids"`
	FilterAccount         string             `json:"filter_account,omitempty"`
	WatchedRule           string             `json:"watched_rule,omitempty"`
	Shortlist             *ListChangePreview `json:"shortlist,omitempty"`
	ShortlistEmpty        bool               `json:"shortlist_empty,omitempty"`
	MetadataFilters       *DiscoveryCriteria `json:"metadata_filters,omitempty"`
	MetadataFilterScope   string             `json:"metadata_filter_scope,omitempty"`
	ExcludedMetadata      int                `json:"excluded_metadata"`
	UnknownMetadata       int                `json:"unknown_metadata"`
	MetadataMismatches    int                `json:"metadata_mismatches"`
	UnknownMetadataFields map[string]int     `json:"unknown_metadata_fields,omitempty"`
}

func ValidateDiscovery(o DiscoveryOptions) error {
	if _, err := normalizeDiscoveryCriteria(o.Criteria); err != nil {
		return err
	}
	if len(o.ExcludeIDs) > 10000 {
		return fmt.Errorf("at most 10000 previously seen IDs")
	}
	for _, id := range o.ExcludeIDs {
		if _, err := listID(id); err != nil {
			return err
		}
	}
	if o.Shortlist != "" && strings.TrimSpace(o.Shortlist) == "" {
		return fmt.Errorf("shortlist must be a nonempty list ID or slug")
	}

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

// Discover reads one ranked response. Optional exclusions require complete,
// activity-bounded account reads. It never applies a shortlist mutation.
func (c *APIClient) Discover(o DiscoveryOptions) (DiscoveryResult, error) {
	result := DiscoveryResult{Mode: o.Mode, Type: o.Type, Items: []DiscoveryItem{}}
	if err := ValidateDiscovery(o); err != nil {
		return result, err
	}
	criteria, err := normalizeDiscoveryCriteria(o.Criteria)
	if err != nil {
		return result, err
	}
	if err := c.validateDiscoveryGenres(o.Type, criteria); err != nil {
		return result, err
	}
	if criteria.active() {
		result.MetadataFilters = &criteria
		result.MetadataFilterScope = "returned_page_only"
		result.UnknownMetadataFields = map[string]int{}
	}
	watched, watchlisted, owner, err := c.discoveryExclusions(o)
	if err != nil {
		return result, fmt.Errorf("discovery exclusions unavailable; no partially filtered results: %w", err)
	}
	result.WatchedFiltered = o.ExcludeWatched
	result.WatchlistFiltered = o.ExcludeWatchlisted
	result.FilterAccount = owner
	if o.ExcludeWatched {
		result.WatchedRule = "movies: any history play; shows: any episode history"
	}
	excluded := map[string]bool{}
	for _, id := range o.ExcludeIDs {
		excluded[id] = true
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
	result.SourceCount = len(rows)
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
			Title string          `json:"title"`
			Year  json.RawMessage `json:"year"`
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
			result.DuplicatesRemoved++
			continue
		}
		seen[id] = true
		var year *int
		if json.Unmarshal(title.Year, &year) != nil {
			year = nil
		}
		if year != nil && (*year < 1800 || *year > 9999) {
			year = nil
		}
		if watched[id] {
			result.ExcludedWatched++
			continue
		}
		if watchlisted[id] {
			result.ExcludedWatchlisted++
			continue
		}
		if excluded[id] {
			result.ExcludedIDs++
			continue
		}
		if matches, missing := matchDiscoveryMetadata(media, criteria); !matches {
			result.ExcludedMetadata++
			if len(missing) > 0 {
				result.UnknownMetadata++
				for _, field := range missing {
					result.UnknownMetadataFields[field]++
				}
			} else {
				result.MetadataMismatches++
			}
			continue
		}
		result.Items = append(result.Items, DiscoveryItem{Type: o.Type, TraktID: id, Title: title.Title, Year: year, Watchers: watchers, Media: media})
	}
	result.Pagination = Pagination{Page: response.Header.Get("X-Pagination-Page"), PageCount: response.Header.Get("X-Pagination-Page-Count"), Limit: response.Header.Get("X-Pagination-Limit"), ItemCount: response.Header.Get("X-Pagination-Item-Count")}
	result.PaginationAvailable = result.Page != "" && result.PageCount != "" && result.Pagination.Limit != "" && result.ItemCount != ""
	if o.Shortlist != "" {
		if len(result.Items) == 0 {
			result.ShortlistEmpty = true
		} else {
			ids := []string{}
			for _, item := range result.Items {
				ids = append(ids, item.TraktID)
			}
			preview, err := PreviewListChange("me", ListChange{Action: "add", List: o.Shortlist, Type: o.Type, IDs: ids})
			if err != nil {
				return result, err
			}
			result.Shortlist = &preview
		}
	}
	return result, nil
}
