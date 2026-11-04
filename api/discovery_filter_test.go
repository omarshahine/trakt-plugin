package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestDiscoveryFiltersCompleteReadsAndShortlistPreview(t *testing.T) {
	for _, kind := range []string{"movie", "show"} {
		for _, mode := range []string{"success", "activity_changed", "incomplete", "null_history", "no_show", "duplicate_event", "bad_watchlist", "wrong_status", "empty_result"} {
			t.Run(kind+"_"+mode, func(t *testing.T) {
				markers, catalogCalls := 0, 0
				c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						t.Fatal("unexpected mutation/refresh")
					}
					switch r.URL.Path {
					case "/users/settings":
						fmt.Fprint(w, `{"user":{"ids":{"slug":"sample"}}}`)
					case "/sync/last_activities":
						markers++
						if mode == "activity_changed" && markers == 2 {
							fmt.Fprint(w, `{"all":"changed"}`)
						} else {
							fmt.Fprint(w, `{"all":"same"}`)
						}
					case "/users/sample/history":
						if mode == "wrong_status" {
							w.WriteHeader(401)
							return
						}
						page, _ := strconv.Atoi(r.URL.Query().Get("page"))
						w.Header().Set("X-Pagination-Page", strconv.Itoa(page))
						w.Header().Set("X-Pagination-Page-Count", "2")
						w.Header().Set("X-Pagination-Item-Count", "2")
						w.Header().Set("X-Pagination-Limit", "1")
						if mode == "incomplete" {
							w.Header().Del("X-Pagination-Item-Count")
						}
						if mode == "null_history" {
							fmt.Fprint(w, `null`)
							return
						}
						id := page
						media := 55
						if page == 2 {
							media = 56
						}
						if mode == "duplicate_event" {
							id = 1
						}
						if kind == "movie" {
							fmt.Fprintf(w, `[{"id":%d,"type":"movie","movie":{"ids":{"trakt":%d}}}]`, id, media)
						} else if mode == "no_show" {
							fmt.Fprintf(w, `[{"id":%d,"type":"episode"}]`, id)
						} else {
							fmt.Fprintf(w, `[{"id":%d,"type":"episode","episode":{"ids":{"trakt":155}},"show":{"ids":{"trakt":%d}}}]`, id, media)
						}
					case "/users/sample/watchlist":
						w.Header().Set("X-Pagination-Page", "1")
						w.Header().Set("X-Pagination-Page-Count", "1")
						w.Header().Set("X-Pagination-Item-Count", "1")
						w.Header().Set("X-Pagination-Limit", "1000")
						if mode == "bad_watchlist" {
							fmt.Fprint(w, `[{"id":99,"type":"person"}]`)
						} else {
							fmt.Fprintf(w, `[{"id":99,"type":"%s","%s":{"ids":{"trakt":57}}}]`, kind, kind)
						}
					case "/movies/popular", "/shows/popular":
						catalogCalls++
						rows := []any{}
						for _, id := range []int{55, 56, 57, 58, 59, 59} {
							rows = append(rows, map[string]any{"title": "Synthetic " + strconv.Itoa(id), "ids": map[string]int{"trakt": id}})
						}
						if mode == "empty_result" {
							rows = rows[:4]
						}
						if err := json.NewEncoder(w).Encode(rows); err != nil {
							t.Error(err)
						}
					default:
						t.Errorf("unexpected path %s", r.URL.Path)
						w.WriteHeader(500)
					}
				})
				c.Credentials.RefreshToken = "SYNTHETIC-REFRESH"
				r, err := c.Discover(DiscoveryOptions{Mode: "popular", Type: kind, Page: 1, Limit: 10, ExcludeWatched: true, ExcludeWatchlisted: true, ExcludeIDs: []string{"58"}, Shortlist: "synthetic-list"})
				good := mode == "success" || mode == "empty_result" || (mode == "no_show" && kind == "movie")
				if !good {
					if err == nil || catalogCalls != 0 {
						t.Fatalf("failed-open filters: %+v %v calls=%d", r, err, catalogCalls)
					}
					return
				}
				if err != nil || !r.WatchedFiltered || !r.WatchlistFiltered || r.ExcludedWatched != 2 || r.ExcludedWatchlisted != 1 || r.ExcludedIDs != 1 || r.FilterAccount != "sample" {
					t.Fatalf("bad filter %+v %v", r, err)
				}
				if mode == "empty_result" {
					if len(r.Items) != 0 || r.Shortlist != nil || !r.ShortlistEmpty {
						t.Fatal("empty shortlist became write", r)
					}
					return
				}
				if len(r.Items) != 1 || r.Items[0].TraktID != "59" || r.DuplicatesRemoved != 1 || r.Shortlist == nil || r.Shortlist.Applied || !strings.HasSuffix(r.Shortlist.Path, "/synthetic-list/items") {
					t.Fatal("bad shortlist", r)
				}
			})
		}
	}
}
func TestDiscoveryExplicitSeenIDsDeduplicateWithoutAccountReads(t *testing.T) {
	calls := 0
	c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/movies/popular" {
			t.Fatal("unrequested account read")
		}
		fmt.Fprint(w, `[{"title":"first","ids":{"trakt":55}},{"title":"duplicate","ids":{"trakt":55}},{"title":"keep","ids":{"trakt":56}}]`)
	})
	r, err := c.Discover(DiscoveryOptions{Mode: "popular", Type: "movie", Page: 1, Limit: 10, ExcludeIDs: []string{"55", "55"}})
	if err != nil || calls != 1 || len(r.Items) != 1 || r.Items[0].TraktID != "56" || r.ExcludedIDs != 1 || r.DuplicatesRemoved != 1 || r.WatchedFiltered || r.FilterAccount != "" {
		t.Fatal(r, err)
	}
	if err := ValidateDiscovery(DiscoveryOptions{Mode: "popular", Type: "movie", Page: 1, Limit: 10, ExcludeIDs: []string{"055"}}); err == nil {
		t.Fatal("invalid seen id")
	}
}

func TestWatchlistedSeasonEpisodeExcludesOnlyParentShow(t *testing.T) {
	for _, entryType := range []string{"season", "episode"} {
		for _, kind := range []string{"movie", "show"} {
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/users/settings":
					fmt.Fprint(w, `{"user":{"ids":{"slug":"sample"}}}`)
				case "/sync/last_activities":
					fmt.Fprint(w, `{"all":"same"}`)
				case "/users/sample/watchlist":
					w.Header().Set("X-Pagination-Page", "1")
					w.Header().Set("X-Pagination-Page-Count", "1")
					w.Header().Set("X-Pagination-Item-Count", "1")
					w.Header().Set("X-Pagination-Limit", "1000")
					fmt.Fprintf(w, `[{"id":99,"type":"%s","%s":{"ids":{"trakt":155}},"show":{"ids":{"trakt":55}}}]`, entryType, entryType)
				case "/movies/popular", "/shows/popular":
					fmt.Fprint(w, `[{"title":"same numeric ID","ids":{"trakt":55}}]`)
				default:
					t.Fatal("unexpected request", r.URL.Path)
				}
			})
			r, err := c.Discover(DiscoveryOptions{Mode: "popular", Type: kind, Page: 1, Limit: 10, ExcludeWatchlisted: true})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if kind == "movie" {
				want = 1
			}
			if len(r.Items) != want {
				t.Fatal("typed parent exclusion failed", entryType, kind, r)
			}
		}
	}
}
