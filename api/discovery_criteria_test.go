package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
)

func criteriaInt(x int) *int           { return &x }
func criteriaFloat(x float64) *float64 { return &x }
func criteriaVotes(x int64) *int64     { return &x }
func TestMetadataCriteriaAcrossModesAndTypes(t *testing.T) {
	for _, kind := range []string{"movie", "show"} {
		for _, mode := range []string{"popular", "trending", "related", "recommendations"} {
			t.Run(kind+mode, func(t *testing.T) {
				genreCalls, catalogCalls := 0, 0
				c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						t.Fatal("unexpected write/refresh")
					}
					if r.URL.Path == "/genres/"+kind+"s" {
						genreCalls++
						fmt.Fprint(w, `[{"name":"Action","slug":"action"},{"name":"Drama","slug":"drama"}]`)
						return
					}
					catalogCalls++
					if r.URL.Query().Get("genres") != "" || r.URL.Query().Get("years") != "" || r.URL.Query().Get("ratings") != "" {
						t.Fatal("undocumented server filter sent")
					}
					rows := []any{}
					for i := 1; i <= 8; i++ {
						m := map[string]any{"title": "Synthetic", "ids": map[string]int{"trakt": i}, "year": 2020, "genres": []string{"action"}, "runtime": 90, "rating": 8.0, "votes": 500}
						switch i {
						case 2:
							m["genres"] = []string{"drama"}
						case 3:
							m["year"] = nil
							m["genres"] = []string{}
							m["runtime"] = 0
							m["rating"] = 0
							m["votes"] = nil
						case 4:
							m["runtime"] = 200
						case 5:
							m["rating"] = 6.0
						case 6:
							m["votes"] = 99
						case 7:
							m["year"] = 2000
						case 8:
							m["year"] = "invalid"
							m["rating"] = nil
						}
						if mode == "trending" {
							rows = append(rows, map[string]any{kind: m, "watchers": 3})
						} else {
							rows = append(rows, m)
						}
					}
					rows = append(rows, rows[0])
					if err := json.NewEncoder(w).Encode(rows); err != nil {
						t.Error(err)
					}
				})
				id := ""
				if mode == "related" {
					id = "55"
				}
				r, err := c.Discover(DiscoveryOptions{Mode: mode, Type: kind, ID: id, Page: 1, Limit: 10, Criteria: DiscoveryCriteria{Genres: []string{" Action ", "action"}, YearMin: criteriaInt(2010), YearMax: criteriaInt(2025), RuntimeMin: criteriaInt(60), RuntimeMax: criteriaInt(120), MinRating: criteriaFloat(7), MinVotes: criteriaVotes(100)}})
				if err != nil || genreCalls != 1 || catalogCalls != 1 || len(r.Items) != 1 || r.Items[0].TraktID != "1" || r.SourceCount != 9 || r.DuplicatesRemoved != 1 || r.ExcludedMetadata != 7 || r.UnknownMetadata != 2 || r.MetadataMismatches != 5 || r.MetadataFilterScope != "returned_page_only" {
					t.Fatal(r, err, genreCalls, catalogCalls)
				}
				if r.UnknownMetadataFields["runtime"] != 1 || r.UnknownMetadataFields["year"] != 2 || r.UnknownMetadataFields["rating"] != 2 || r.MetadataFilters.Genres[0] != "action" {
					t.Fatal("missing-data accounting wrong", r)
				}
			})
		}
	}
}
func TestMetadataUnknownAndBoundaryPolicy(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		f       DiscoveryCriteria
		keep    bool
		missing string
	}{
		{`{"rating":0}`, DiscoveryCriteria{MinRating: criteriaFloat(0)}, false, "rating"},
		{`{"votes":0}`, DiscoveryCriteria{MinVotes: criteriaVotes(0)}, true, ""},
		{`{"runtime":0}`, DiscoveryCriteria{RuntimeMax: criteriaInt(120)}, false, "runtime"},
		{`{"runtime":120}`, DiscoveryCriteria{RuntimeMax: criteriaInt(120)}, true, ""},
		{`{"runtime":43200}`, DiscoveryCriteria{RuntimeMin: criteriaInt(10000)}, true, ""},
		{`{"rating":7.5}`, DiscoveryCriteria{MinRating: criteriaFloat(7.5)}, true, ""},
		{`{"votes":100}`, DiscoveryCriteria{MinVotes: criteriaVotes(100)}, true, ""},
		{`{"runtime":"90"}`, DiscoveryCriteria{RuntimeMin: criteriaInt(60)}, false, "runtime"},
		{`{}`, DiscoveryCriteria{}, true, ""},
		{`{"genres":[""]}`, DiscoveryCriteria{Genres: []string{"drama"}}, false, "genres"},
		{`{"genres":["drama","thriller"]}`, DiscoveryCriteria{Genres: []string{"action", "drama"}}, true, ""},
	} {
		keep, missing := matchDiscoveryMetadata(json.RawMessage(tc.raw), tc.f)
		if keep != tc.keep || (tc.missing != "" && (len(missing) != 1 || missing[0] != tc.missing)) {
			t.Fatal(tc, keep, missing)
		}
	}
}
func TestMetadataInvalidCriteriaAndGenreFailures(t *testing.T) {
	for _, f := range []DiscoveryCriteria{{YearMin: criteriaInt(0)}, {YearMin: criteriaInt(2025), YearMax: criteriaInt(2020)}, {RuntimeMin: criteriaInt(0)}, {RuntimeMin: criteriaInt(120), RuntimeMax: criteriaInt(90)}, {MinRating: criteriaFloat(math.NaN())}, {MinRating: criteriaFloat(math.Inf(1))}, {MinRating: criteriaFloat(11)}, {MinVotes: criteriaVotes(-1)}, {Genres: []string{"../settings"}}} {
		if _, err := normalizeDiscoveryCriteria(f); err == nil {
			t.Fatal("invalid criteria accepted")
		}
	}
	for _, body := range []string{`[]`, `null`, `{}`, `[{"name":"Action","slug":"action"},{"name":"Action","slug":"action"}]`} {
		calls := 0
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/genres/movies" {
				t.Fatal("catalog called after invalid/unknown genre")
			}
			fmt.Fprint(w, body)
		})
		_, err := c.Discover(DiscoveryOptions{Mode: "popular", Type: "movie", Page: 1, Limit: 10, Criteria: DiscoveryCriteria{Genres: []string{"action"}}})
		if err == nil || calls != 1 {
			t.Fatal("genre failure not explicit", err, calls)
		}
	}
}
