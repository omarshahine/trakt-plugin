package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

type DiscoveryCriteria struct {
	Genres     []string `json:"genres,omitempty"`
	YearMin    *int     `json:"year_min,omitempty"`
	YearMax    *int     `json:"year_max,omitempty"`
	RuntimeMin *int     `json:"runtime_min,omitempty"`
	RuntimeMax *int     `json:"runtime_max,omitempty"`
	MinRating  *float64 `json:"min_rating,omitempty"`
	MinVotes   *int64   `json:"min_votes,omitempty"`
}

func (f DiscoveryCriteria) active() bool {
	return len(f.Genres) > 0 || f.YearMin != nil || f.YearMax != nil || f.RuntimeMin != nil || f.RuntimeMax != nil || f.MinRating != nil || f.MinVotes != nil
}

var genreSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func normalizeDiscoveryCriteria(f DiscoveryCriteria) (DiscoveryCriteria, error) {
	if len(f.Genres) > 50 {
		return f, fmt.Errorf("at most 50 genre slugs")
	}
	unique := map[string]bool{}
	genres := []string{}
	for _, g := range f.Genres {
		g = strings.ToLower(strings.TrimSpace(g))
		if !genreSlug.MatchString(g) {
			return f, fmt.Errorf("genre must be a Trakt slug such as science-fiction")
		}
		if !unique[g] {
			unique[g] = true
			genres = append(genres, g)
		}
	}
	sort.Strings(genres)
	f.Genres = genres
	for _, p := range []*int{f.YearMin, f.YearMax} {
		if p != nil && (*p < 1800 || *p > 9999) {
			return f, fmt.Errorf("year bounds must be 1800-9999")
		}
	}
	for _, p := range []*int{f.RuntimeMin, f.RuntimeMax} {
		if p != nil && (*p < 1 || *p > 10000) {
			return f, fmt.Errorf("runtime bounds must be 1-10000 minutes")
		}
	}
	if f.YearMin != nil && f.YearMax != nil && *f.YearMin > *f.YearMax {
		return f, fmt.Errorf("year-min exceeds year-max")
	}
	if f.RuntimeMin != nil && f.RuntimeMax != nil && *f.RuntimeMin > *f.RuntimeMax {
		return f, fmt.Errorf("runtime-min exceeds runtime-max")
	}
	if f.MinRating != nil && (math.IsNaN(*f.MinRating) || math.IsInf(*f.MinRating, 0) || *f.MinRating < 0 || *f.MinRating > 10) {
		return f, fmt.Errorf("min-rating must be finite, 0-10")
	}
	if f.MinVotes != nil && *f.MinVotes < 0 {
		return f, fmt.Errorf("min-votes must be nonnegative")
	}
	return f, nil
}

type Genre struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (c *APIClient) GetDiscoveryGenres(kind string) ([]Genre, error) {
	if kind != "movie" && kind != "show" {
		return nil, fmt.Errorf("genre type must be movie or show")
	}
	r, err := c.doRequest(requestParams{method: http.MethodGet, path: "/genres/" + kind + "s", auth: true, noAutoRefresh: true, noCache: true})
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("genre read HTTP %d", r.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, fmt.Errorf("genre response unreadable or too large")
	}
	var genres []Genre
	if json.Unmarshal(data, &genres) != nil || genres == nil || len(genres) > 200 {
		return nil, fmt.Errorf("invalid genre catalog")
	}
	seen := map[string]bool{}
	for _, g := range genres {
		if !genreSlug.MatchString(g.Slug) || g.Name == "" || seen[g.Slug] {
			return nil, fmt.Errorf("invalid/duplicate genre")
		}
		seen[g.Slug] = true
	}
	return genres, nil
}
func (c *APIClient) validateDiscoveryGenres(kind string, f DiscoveryCriteria) error {
	if len(f.Genres) == 0 {
		return nil
	}
	available, err := c.GetDiscoveryGenres(kind)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, g := range available {
		known[g.Slug] = true
	}
	for _, g := range f.Genres {
		if !known[g] {
			return fmt.Errorf("unknown %s genre %q; use genres to inspect supported Trakt slugs", kind, g)
		}
	}
	return nil
}

// All selected criteria are ANDed; genre choices are ORed. Missing/invalid active
// metadata is excluded, never inferred as zero, unlimited runtime or a match.
func matchDiscoveryMetadata(raw json.RawMessage, f DiscoveryCriteria) (bool, []string) {
	if !f.active() {
		return true, nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false, []string{"metadata"}
	}
	missing := []string{}
	matches := true
	if len(f.Genres) > 0 {
		var values []string
		if json.Unmarshal(m["genres"], &values) != nil || len(values) == 0 {
			missing = append(missing, "genres")
		} else {
			found := false
			validGenre := false
			for _, a := range values {
				if !genreSlug.MatchString(a) {
					continue
				}
				validGenre = true
				for _, b := range f.Genres {
					if a == b {
						found = true
					}
				}
			}
			if !validGenre {
				missing = append(missing, "genres")
			} else if !found {
				matches = false
			}
		}
	}
	if f.YearMin != nil || f.YearMax != nil {
		var year *int
		if json.Unmarshal(m["year"], &year) != nil || year == nil || *year < 1800 || *year > 9999 {
			missing = append(missing, "year")
		} else if (f.YearMin != nil && *year < *f.YearMin) || (f.YearMax != nil && *year > *f.YearMax) {
			matches = false
		}
	}
	if f.RuntimeMin != nil || f.RuntimeMax != nil {
		var runtime *int
		if json.Unmarshal(m["runtime"], &runtime) != nil || runtime == nil || *runtime <= 0 {
			missing = append(missing, "runtime")
		} else if (f.RuntimeMin != nil && *runtime < *f.RuntimeMin) || (f.RuntimeMax != nil && *runtime > *f.RuntimeMax) {
			matches = false
		}
	}
	if f.MinRating != nil {
		var rating *float64
		if json.Unmarshal(m["rating"], &rating) != nil || rating == nil || *rating <= 0 || *rating > 10 {
			missing = append(missing, "rating")
		} else if *rating < *f.MinRating {
			matches = false
		}
	}
	if f.MinVotes != nil {
		var votes *int64
		if json.Unmarshal(m["votes"], &votes) != nil || votes == nil || *votes < 0 {
			missing = append(missing, "votes")
		} else if *votes < *f.MinVotes {
			matches = false
		}
	}
	return matches && len(missing) == 0, missing
}
