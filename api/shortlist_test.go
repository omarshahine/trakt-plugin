package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestBoundedShortlistStopsReuseSnapshotsAndRetainPartial(t *testing.T) {
	for _, mode := range []string{"target", "budget", "exhausted", "no_headers", "bad_headers", "rate", "activity_changed", "recommendations"} {
		t.Run(mode, func(t *testing.T) {
			pages, history, watchlist, genres, markers := 0, 0, 0, 0, 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("unexpected write/refresh")
				}
				switch r.URL.Path {
				case "/users/settings":
					fmt.Fprint(w, `{"user":{"ids":{"slug":"sample"}}}`)
					return
				case "/sync/last_activities":
					markers++
					if mode == "activity_changed" && markers == 4 {
						fmt.Fprint(w, `{"all":"changed"}`)
					} else {
						fmt.Fprint(w, `{"all":"same"}`)
					}
					return
				case "/genres/movies":
					genres++
					fmt.Fprint(w, `[{"name":"Drama","slug":"drama"}]`)
					return
				case "/users/sample/history":
					history++
					w.Header().Set("X-Pagination-Page", "1")
					w.Header().Set("X-Pagination-Limit", "1000")
					w.Header().Set("X-Pagination-Page-Count", "1")
					w.Header().Set("X-Pagination-Item-Count", "1")
					fmt.Fprint(w, `[{"id":101,"type":"movie","movie":{"ids":{"trakt":99}}}]`)
					return
				case "/users/sample/watchlist":
					watchlist++
					w.Header().Set("X-Pagination-Page", "1")
					w.Header().Set("X-Pagination-Limit", "1000")
					w.Header().Set("X-Pagination-Page-Count", "1")
					w.Header().Set("X-Pagination-Item-Count", "1")
					fmt.Fprint(w, `[{"id":201,"type":"movie","movie":{"ids":{"trakt":98}}}]`)
					return
				case "/movies/popular", "/recommendations/movies":
				default:
					t.Fatal("unexpected path", r.URL.Path)
				}
				pages++
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if mode == "recommendations" {
					if page != 0 {
						t.Fatal("recommendation page fabricated")
					}
					page = 1
				}
				if page == 3 && mode == "rate" {
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(429)
					return
				}
				if mode != "no_headers" {
					w.Header().Set("X-Pagination-Page", strconv.Itoa(page))
					w.Header().Set("X-Pagination-Limit", "3")
					w.Header().Set("X-Pagination-Page-Count", "3")
					w.Header().Set("X-Pagination-Item-Count", "9")
				}
				if mode == "bad_headers" && page == 3 {
					w.Header().Set("X-Pagination-Page", "1")
				}
				ids := [][]int{{99, 98, 55}, {56, 56, 57}, {56, 58, 59}}[page-1]
				if mode == "recommendations" {
					ids = []int{56, 57, 58}
				}
				rows := []any{}
				for _, id := range ids {
					year := 2020
					if id == 55 {
						year = 2000
					}
					rows = append(rows, map[string]any{"title": "Synthetic " + strconv.Itoa(id), "ids": map[string]int{"trakt": id}, "year": year, "genres": []string{"drama"}, "runtime": 90, "rating": 8, "votes": 1000})
				}
				if err := json.NewEncoder(w).Encode(rows); err != nil {
					t.Error(err)
				}
			})
			o := ShortlistOptions{DiscoveryOptions: DiscoveryOptions{Mode: "popular", Type: "movie", Page: 1, Limit: 3, ExcludeWatched: true, ExcludeWatchlisted: true, Shortlist: "55", Criteria: DiscoveryCriteria{Genres: []string{"drama"}, YearMin: criteriaInt(2010), RuntimeMax: criteriaInt(120), MinRating: criteriaFloat(7), MinVotes: criteriaVotes(100)}}, Count: 3, MaxPages: 3}
			if mode == "budget" {
				o.MaxPages = 2
			}
			if mode == "exhausted" {
				o.Count = 10
			}
			if mode == "recommendations" {
				o.Mode = "recommendations"
				o.Count = 5
			}
			c.Credentials.RefreshToken = "SYNTHETIC-REFRESH"
			r, err := c.BuildShortlist(o)
			bad := mode == "rate" || mode == "bad_headers" || mode == "activity_changed"
			if bad {
				if err == nil || r.ScanSucceeded || r.ListAddPreview != nil {
					t.Fatal("unsafe partial success", r, err)
				}
				if mode != "activity_changed" && (len(r.Items) != 2 || r.LastRequestedPage != 3) {
					t.Fatal("partial matches lost", r)
				}
				if mode == "rate" && pages != 3 {
					t.Fatal("rate-limit replay")
				}
				return
			}
			if err != nil || !r.ScanSucceeded || r.CatalogComplete || history != 1 || watchlist != 1 || genres != 1 || markers != 4 {
				t.Fatalf("bad bounded scan %+v %v calls=%d/%d/%d/%d", r, err, history, watchlist, genres, markers)
			}
			expected := map[string]string{"target": "target_met", "budget": "page_budget", "exhausted": "end_of_reported_pages", "no_headers": "pagination_unavailable", "recommendations": "recommendations_single_response"}[mode]
			if r.StopReason != expected {
				t.Fatal("wrong stop", r)
			}
			if mode == "target" {
				if len(r.Items) != 3 || r.PagesRead != 3 || r.SourceRows != 9 || r.DuplicatesRemoved != 2 || r.MatchesObserved != 4 || r.Items[2].TraktID != "58" || !r.CountMet || r.ListAddPreview == nil || r.ListAddPreview.Applied {
					t.Fatal("wrong shortlist", r)
				}
				if !strings.Contains(strings.Join(r.Items[0].FactualFit, ";"), "No movie play in checked history snapshot") {
					t.Fatal("missing factual exclusion evidence")
				}
			}
			if mode == "budget" && (pages != 2 || r.CountMet || len(r.Items) != 2) {
				t.Fatal("budget ignored", r)
			}
			if mode == "no_headers" && pages != 1 {
				t.Fatal("blind pagination")
			}
			if mode == "recommendations" && (pages != 1 || len(r.Items) != 3 || r.EffectivePageBudget != 1) {
				t.Fatal("recommendation pagination fabricated", r)
			}
		})
	}
}
func TestShortlistBoundsAndPagination(t *testing.T) {
	valid := ShortlistOptions{DiscoveryOptions: DiscoveryOptions{Mode: "popular", Type: "movie", Page: 1, Limit: 20}, Count: 5, MaxPages: 5}
	for _, change := range []func(*ShortlistOptions){func(o *ShortlistOptions) { o.Count = 0 }, func(o *ShortlistOptions) { o.Count = 51 }, func(o *ShortlistOptions) { o.MaxPages = 21 }, func(o *ShortlistOptions) { o.Limit = 101 }, func(o *ShortlistOptions) { o.Page = 1000001 }} {
		o := valid
		change(&o)
		if _, err := (&APIClient{}).BuildShortlist(o); err == nil {
			t.Fatal("unbounded request")
		}
	}
	for _, p := range []DiscoveryResult{{Pagination: Pagination{Page: "1"}}, {SourceCount: 1, Pagination: Pagination{Page: "1", PageCount: "3", Limit: "3", ItemCount: "9"}}, {SourceCount: 0, Pagination: Pagination{Page: "1", PageCount: "2", Limit: "3", ItemCount: "0"}}} {
		if _, err := shortlistCanAdvance(p, 1); err == nil {
			t.Fatal("bad pagination accepted", p)
		}
	}
	if next, err := shortlistCanAdvance(DiscoveryResult{SourceCount: 0, Pagination: Pagination{Page: "1", PageCount: "0", Limit: "3", ItemCount: "0"}}, 1); err != nil || next {
		t.Fatal("valid empty pagination refused", err)
	}
}
