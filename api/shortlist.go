package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ShortlistOptions struct {
	DiscoveryOptions
	Count    int
	MaxPages int
}
type ShortlistItem struct {
	DiscoveryItem
	RequestedPage int      `json:"requested_page"`
	FactualFit    []string `json:"factual_fit"`
}
type ShortlistResult struct {
	Kind                  string             `json:"kind"`
	Type                  string             `json:"type"`
	Mode                  string             `json:"mode"`
	AsOf                  string             `json:"as_of"`
	Wanted                int                `json:"wanted"`
	CountMet              bool               `json:"count_met"`
	ScanSucceeded         bool               `json:"scan_succeeded"`
	StopReason            string             `json:"stop_reason"`
	CatalogComplete       bool               `json:"catalog_complete"`
	MaxPages              int                `json:"max_pages"`
	EffectivePageBudget   int                `json:"effective_page_budget"`
	PagesRead             int                `json:"pages_read"`
	SourceRows            int                `json:"source_rows"`
	MatchesObserved       int                `json:"matches_observed"`
	DuplicatesRemoved     int                `json:"duplicates_removed"`
	ExcludedWatched       int                `json:"excluded_watched"`
	ExcludedWatchlisted   int                `json:"excluded_watchlisted"`
	ExcludedIDs           int                `json:"excluded_ids"`
	ExcludedMetadata      int                `json:"excluded_metadata"`
	UnknownMetadata       int                `json:"unknown_metadata"`
	MetadataMismatches    int                `json:"metadata_mismatches"`
	UnknownMetadataFields map[string]int     `json:"unknown_metadata_fields,omitempty"`
	FilterAccount         string             `json:"filter_account,omitempty"`
	WatchedFiltered       bool               `json:"watched_filtered"`
	WatchlistFiltered     bool               `json:"watchlist_filtered"`
	Criteria              DiscoveryCriteria  `json:"criteria"`
	ExplanationScope      string             `json:"explanation_scope"`
	Items                 []ShortlistItem    `json:"items"`
	ListAddPreview        *ListChangePreview `json:"list_add_preview,omitempty"`
	StartPage             int                `json:"start_page"`
	LastRequestedPage     int                `json:"last_requested_page"`
}

func ValidateShortlist(o ShortlistOptions) error {
	if o.Count < 1 || o.Count > 50 || o.MaxPages < 1 || o.MaxPages > 20 {
		return fmt.Errorf("count must be 1-50 and max-pages 1-20")
	}
	if o.Page > 1000000 {
		return fmt.Errorf("starting page exceeds shortlist bound")
	}
	return ValidateDiscovery(o.DiscoveryOptions)
}
func (c *APIClient) BuildShortlist(o ShortlistOptions) (ShortlistResult, error) {
	r := ShortlistResult{Kind: "watch_tonight", StartPage: o.Page, Type: o.Type, Mode: o.Mode, AsOf: time.Now().UTC().Format(time.RFC3339Nano), Wanted: o.Count, MaxPages: o.MaxPages, EffectivePageBudget: o.MaxPages, Items: []ShortlistItem{}, UnknownMetadataFields: map[string]int{}, ExplanationScope: "metadata_and_checked_records_only"}
	if err := ValidateShortlist(o); err != nil {
		r.StopReason = "invalid_options"
		return r, err
	}
	reader := *c
	reader.Credentials.RefreshToken = ""
	c = &reader
	var stamp json.RawMessage
	var err error
	if o.ExcludeWatched || o.ExcludeWatchlisted {
		stamp, err = c.GetBackupActivity()
		if err != nil {
			r.StopReason = "prepare_failed"
			return r, err
		}
	}
	ctx, err := c.prepareDiscovery(o.DiscoveryOptions)
	if err != nil {
		r.StopReason = "prepare_failed"
		return r, err
	}
	r.Criteria = ctx.criteria
	r.FilterAccount = ctx.owner
	r.WatchedFiltered = o.ExcludeWatched
	r.WatchlistFiltered = o.ExcludeWatchlisted
	if o.Mode == "recommendations" {
		r.EffectivePageBudget = 1
	}
	for n := 0; n < r.EffectivePageBudget; n++ {
		query := o.DiscoveryOptions
		query.Page = o.Page + n
		r.LastRequestedPage = query.Page
		query.Shortlist = ""
		page, err := c.discoverPrepared(query, ctx)
		if err != nil {
			r.StopReason = "request_failed"
			return r, err
		}
		next, err := shortlistCanAdvance(page, query.Page)
		if err != nil {
			r.StopReason = "invalid_pagination"
			return r, err
		}
		r.PagesRead++
		r.SourceRows += page.SourceCount
		r.MatchesObserved += len(page.Items)
		r.DuplicatesRemoved += page.DuplicatesRemoved
		r.ExcludedWatched += page.ExcludedWatched
		r.ExcludedWatchlisted += page.ExcludedWatchlisted
		r.ExcludedIDs += page.ExcludedIDs
		r.ExcludedMetadata += page.ExcludedMetadata
		r.UnknownMetadata += page.UnknownMetadata
		r.MetadataMismatches += page.MetadataMismatches
		for k, v := range page.UnknownMetadataFields {
			r.UnknownMetadataFields[k] += v
		}
		for _, item := range page.Items {
			if len(r.Items) == o.Count {
				break
			}
			r.Items = append(r.Items, ShortlistItem{DiscoveryItem: item, RequestedPage: query.Page, FactualFit: shortlistFacts(item, query, ctx.criteria)})
		}
		if len(r.Items) == o.Count {
			r.CountMet = true
			r.StopReason = "target_met"
			break
		}
		if o.Mode == "recommendations" {
			r.StopReason = "recommendations_single_response"
			break
		}
		if !page.PaginationAvailable {
			r.StopReason = "pagination_unavailable"
			break
		}
		if !next {
			r.StopReason = "end_of_reported_pages"
			break
		}
		r.StopReason = "page_budget"
	}
	if o.ExcludeWatched || o.ExcludeWatchlisted {
		end, err := c.GetBackupActivity()
		if err != nil {
			r.StopReason = "exclusions_unverified"
			return r, err
		}
		if !bytes.Equal(stamp, end) {
			r.StopReason = "exclusions_changed"
			return r, fmt.Errorf("account activity changed during shortlist scan; retained matches are partial, no list preview")
		}
	}
	r.ScanSucceeded = true
	if o.Shortlist != "" && len(r.Items) > 0 {
		ids := []string{}
		for _, x := range r.Items {
			ids = append(ids, x.TraktID)
		}
		preview, err := PreviewListChange("me", ListChange{Action: "add", List: o.Shortlist, Type: o.Type, IDs: ids})
		if err != nil {
			r.ScanSucceeded = false
			return r, err
		}
		r.ListAddPreview = &preview
	}
	return r, nil
}

// Pagination is used only when structurally consistent. Missing headers stop
// bounded scanning; they never imply empty catalog or authorize blind page loops.
func shortlistCanAdvance(p DiscoveryResult, requested int) (bool, error) {
	fields := []string{p.Page, p.PageCount, p.Pagination.Limit, p.ItemCount}
	present := 0
	for _, v := range fields {
		if v != "" {
			present++
		}
	}
	if present == 0 {
		return false, nil
	}
	if present != 4 {
		return false, fmt.Errorf("incomplete catalog pagination")
	}
	values := []int{}
	for _, v := range fields {
		n, err := strconv.Atoi(v)
		if err != nil {
			return false, fmt.Errorf("invalid catalog pagination")
		}
		values = append(values, n)
	}
	page, pages, limit, total := values[0], values[1], values[2], values[3]
	if page != requested || pages < 0 || limit < 1 || limit > 100 || total < 0 {
		return false, fmt.Errorf("mismatched catalog pagination")
	}
	wantPages := total / limit
	if total%limit != 0 {
		wantPages++
	}
	if total == 0 {
		if pages > 1 || p.SourceCount != 0 {
			return false, fmt.Errorf("inconsistent empty pagination")
		}
		return false, nil
	}
	if pages != wantPages {
		return false, fmt.Errorf("catalog page total mismatch")
	}
	want := limit
	if page == pages {
		want = total - limit*(pages-1)
	}
	if page > pages {
		want = 0
	}
	if p.SourceCount != want {
		return false, fmt.Errorf("catalog response size differs from pagination")
	}
	return page < pages, nil
}
func shortlistFacts(item DiscoveryItem, o DiscoveryOptions, f DiscoveryCriteria) []string {
	facts := []string{fmt.Sprintf("From %s %s results, requested page %d", o.Mode, o.Type, o.Page)}
	var m struct {
		Year    *int     `json:"year"`
		Runtime *int     `json:"runtime"`
		Rating  *float64 `json:"rating"`
		Votes   *int64   `json:"votes"`
		Genres  []string `json:"genres"`
	}
	_ = json.Unmarshal(item.Media, &m)
	if len(f.Genres) > 0 {
		matched := []string{}
		for _, g := range f.Genres {
			for _, v := range m.Genres {
				if g == v {
					matched = append(matched, g)
					break
				}
			}
		}
		facts = append(facts, "Matches requested genre: "+strings.Join(matched, ", "))
	}
	if (f.YearMin != nil || f.YearMax != nil) && m.Year != nil {
		label := "Catalog year"
		if item.Type == "show" {
			label = "Series premiere year"
		}
		facts = append(facts, fmt.Sprintf("%s %d meets requested range", label, *m.Year))
	}
	if (f.RuntimeMin != nil || f.RuntimeMax != nil) && m.Runtime != nil {
		label := "Movie runtime"
		if item.Type == "show" {
			label = "Per-episode runtime metadata"
		}
		facts = append(facts, fmt.Sprintf("%s %d minutes meets requested bounds", label, *m.Runtime))
	}
	if f.MinRating != nil && m.Rating != nil {
		facts = append(facts, "Community rating "+strconv.FormatFloat(*m.Rating, 'f', -1, 64)+"/10 meets requested minimum")
	}
	if f.MinVotes != nil && m.Votes != nil {
		facts = append(facts, fmt.Sprintf("%d community votes meets requested minimum", *m.Votes))
	}
	if o.ExcludeWatched {
		phrase := "No movie play in checked history snapshot"
		if item.Type == "show" {
			phrase = "No episode of this show in checked history snapshot"
		}
		facts = append(facts, phrase)
	}
	if o.ExcludeWatchlisted {
		facts = append(facts, "Not present in checked watchlist snapshot")
	}
	return facts
}
