package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"
)

type SeededRelatedOptions struct {
	Type               string `json:"type"`
	Source             string `json:"source"`
	MinSeedRating      int    `json:"min_seed_rating"`
	MaxSeeds           int    `json:"max_seeds"`
	PerSeed            int    `json:"per_seed"`
	Count              int    `json:"count"`
	ExcludeWatchlisted bool   `json:"exclude_watchlisted"`
}
type RelatedSeed struct {
	TraktID  string `json:"trakt_id"`
	Title    string `json:"title"`
	Favorite bool   `json:"favorite"`
	Rating   *int   `json:"rating,omitempty"`
}
type SeededRelatedItem struct {
	DiscoveryItem
	RelatedTo   []RelatedSeed `json:"related_to"`
	Explanation string        `json:"explanation"`
}
type SeededRelatedResult struct {
	Kind                string               `json:"kind"`
	AsOf                string               `json:"as_of"`
	Account             string               `json:"account,omitempty"`
	Options             SeededRelatedOptions `json:"options"`
	ScanSucceeded       bool                 `json:"scan_succeeded"`
	CountMet            bool                 `json:"count_met"`
	StopReason          string               `json:"stop_reason"`
	SeedsAvailable      int                  `json:"seeds_available"`
	Seeds               []RelatedSeed        `json:"seeds"`
	SeedsRead           int                  `json:"seeds_read"`
	SourceRows          int                  `json:"source_rows"`
	ExcludedWatched     int                  `json:"excluded_watched"`
	ExcludedWatchlisted int                  `json:"excluded_watchlisted"`
	ExcludedSeeds       int                  `json:"excluded_seeds"`
	DuplicatesRemoved   int                  `json:"duplicates_removed"`
	CandidatesObserved  int                  `json:"candidates_observed"`
	Items               []SeededRelatedItem  `json:"items"`
}

func ValidateSeededRelated(o SeededRelatedOptions) error {
	if o.Type != "movie" && o.Type != "show" {
		return fmt.Errorf("type must be movie or show")
	}
	if o.Source != "both" && o.Source != "ratings" && o.Source != "favorites" {
		return fmt.Errorf("source must be both, ratings or favorites")
	}
	if o.MinSeedRating < 1 || o.MinSeedRating > 10 || o.MaxSeeds < 1 || o.MaxSeeds > 10 || o.PerSeed < 1 || o.PerSeed > 20 || o.Count < 1 || o.Count > 50 {
		return fmt.Errorf("min-seed-rating 1-10, max-seeds 1-10, per-seed 1-20 and count 1-50 required")
	}
	return nil
}

func (c *APIClient) relatedSeeds(owner string, o SeededRelatedOptions) ([]RelatedSeed, error) {
	all := map[string]RelatedSeed{}
	for _, resource := range []string{"favorites", "ratings"} {
		if o.Source != "both" && o.Source != resource {
			continue
		}
		seen := map[string]bool{}
		err := c.walkStrictRecords("/users/"+url.PathEscape(owner)+"/"+resource+"/"+o.Type+"s", 1000, 10000, func(raw json.RawMessage) error {
			var row map[string]json.RawMessage
			if json.Unmarshal(raw, &row) != nil || row == nil {
				return fmt.Errorf("invalid preference seed row")
			}
			var kind string
			if json.Unmarshal(row["type"], &kind) != nil || kind != o.Type {
				return fmt.Errorf("preference seed type mismatch")
			}
			var media struct {
				Title string `json:"title"`
				IDs   struct {
					Trakt json.Number `json:"trakt"`
				} `json:"ids"`
			}
			if json.Unmarshal(row[kind], &media) != nil || media.Title == "" {
				return fmt.Errorf("preference seed media missing")
			}
			id := media.IDs.Trakt.String()
			if _, err := listID(id); err != nil {
				return err
			}
			if seen[id] {
				return fmt.Errorf("duplicate preference seed")
			}
			seen[id] = true
			seed := all[id]
			seed.TraktID = id
			seed.Title = media.Title
			if resource == "favorites" {
				seed.Favorite = true
			} else {
				var rating *int
				if json.Unmarshal(row["rating"], &rating) != nil || rating == nil || *rating < 1 || *rating > 10 {
					return fmt.Errorf("invalid seed rating")
				}
				seed.Rating = rating
			}
			all[id] = seed
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	seeds := []RelatedSeed{}
	for _, seed := range all {
		if seed.Favorite || (seed.Rating != nil && *seed.Rating >= o.MinSeedRating) {
			seeds = append(seeds, seed)
		}
	}
	sort.Slice(seeds, func(i, j int) bool {
		a, b := seeds[i], seeds[j]
		if a.Favorite != b.Favorite {
			return a.Favorite
		}
		ar, br := 0, 0
		if a.Rating != nil {
			ar = *a.Rating
		}
		if b.Rating != nil {
			br = *b.Rating
		}
		if ar != br {
			return ar > br
		}
		return a.TraktID < b.TraktID
	})
	return seeds, nil
}

// Seed priority then first-seen service order is explicit, not a fabricated
// personalized score. All selected seed responses are read before truncation,
// so duplicate candidates retain each observed seed connection.
func (c *APIClient) BuildSeededRelated(o SeededRelatedOptions) (r SeededRelatedResult, resultErr error) {
	r = SeededRelatedResult{Kind: "seeded_related", AsOf: time.Now().UTC().Format(time.RFC3339Nano), Options: o, Seeds: []RelatedSeed{}, Items: []SeededRelatedItem{}, StopReason: "prepare_failed"}
	defer func() {
		if o.Count > 0 && len(r.Items) > o.Count {
			r.Items = r.Items[:o.Count]
		}
	}()
	if err := ValidateSeededRelated(o); err != nil {
		return r, err
	}
	reader := *c
	reader.Credentials.RefreshToken = ""
	c = &reader
	stamp, err := c.GetBackupActivity()
	if err != nil {
		return r, err
	}
	query := DiscoveryOptions{Mode: "related", Type: o.Type, ID: "1", Page: 1, Limit: o.PerSeed, ExcludeWatched: true, ExcludeWatchlisted: o.ExcludeWatchlisted}
	ctx, err := c.prepareDiscovery(query)
	if err != nil {
		return r, err
	}
	r.Account = ctx.owner
	seeds, err := c.relatedSeeds(ctx.owner, o)
	if err != nil {
		return r, err
	}
	r.SeedsAvailable = len(seeds)
	for _, s := range seeds {
		query.ExcludeIDs = append(query.ExcludeIDs, s.TraktID)
	}
	if len(seeds) > o.MaxSeeds {
		seeds = seeds[:o.MaxSeeds]
	}
	r.Seeds = seeds
	index := map[string]int{}
	for _, seed := range seeds {
		query.ID = seed.TraktID
		ctx.seen = map[string]bool{}
		page, err := c.discoverPrepared(query, ctx)
		if err != nil {
			r.StopReason = "request_failed"
			return r, err
		}
		r.SeedsRead++
		r.SourceRows += page.SourceCount
		r.ExcludedWatched += page.ExcludedWatched
		r.ExcludedWatchlisted += page.ExcludedWatchlisted
		r.ExcludedSeeds += page.ExcludedIDs
		r.DuplicatesRemoved += page.DuplicatesRemoved
		for _, item := range page.Items {
			if at, ok := index[item.TraktID]; ok {
				r.DuplicatesRemoved++
				r.Items[at].RelatedTo = append(r.Items[at].RelatedTo, seed)
				continue
			}
			index[item.TraktID] = len(r.Items)
			r.Items = append(r.Items, SeededRelatedItem{DiscoveryItem: item, RelatedTo: []RelatedSeed{seed}, Explanation: "Trakt returned this title as related to the recorded favorite/high-rating seed(s); no play in checked history. This is a source connection, not a guarantee of enjoyment or availability."})
		}
		r.CandidatesObserved = len(r.Items)
	}
	end, err := c.GetBackupActivity()
	if err != nil {
		r.StopReason = "activity_unverified"
		return r, err
	}
	if !bytes.Equal(stamp, end) {
		r.StopReason = "activity_changed"
		return r, fmt.Errorf("account activity changed during seed/related scan; retained items are unverified partial results")
	}
	if len(r.Items) > o.Count {
		r.Items = r.Items[:o.Count]
	}
	r.CountMet = len(r.Items) == o.Count
	r.ScanSucceeded = true
	r.StopReason = "seed_window_complete"
	if r.SeedsAvailable == 0 {
		r.StopReason = "no_eligible_seeds"
	}
	return r, nil
}
