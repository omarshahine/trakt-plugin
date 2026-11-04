package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSeededRelatedConnectionsBudgetsAndPartialResults(t *testing.T) {
	for _, mode := range []string{"normal", "count", "budget", "none", "duplicate", "ratingnull", "rate", "changed", "favorites", "ratings"} {
		t.Run(mode, func(t *testing.T) {
			markers, related, favorites, ratings := 0, []string{}, 0, 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("unexpected write/refresh")
				}
				paged := func(body string, count int) {
					w.Header().Set("X-Pagination-Page", "1")
					w.Header().Set("X-Pagination-Limit", "1000")
					w.Header().Set("X-Pagination-Page-Count", "1")
					w.Header().Set("X-Pagination-Item-Count", fmt.Sprint(count))
					fmt.Fprint(w, body)
				}
				switch r.URL.Path {
				case "/users/settings":
					fmt.Fprint(w, `{"user":{"ids":{"slug":"sample"}}}`)
				case "/sync/last_activities":
					markers++
					if mode == "changed" && markers == 4 {
						fmt.Fprint(w, `{"all":"changed"}`)
					} else {
						fmt.Fprint(w, `{"all":"same"}`)
					}
				case "/users/sample/history":
					paged(`[{"id":11,"type":"episode","episode":{"ids":{"trakt":900}},"show":{"ids":{"trakt":99}}}]`, 1)
				case "/users/sample/watchlist":
					paged(`[{"id":12,"type":"show","show":{"ids":{"trakt":98}}}]`, 1)
				case "/users/sample/favorites/shows":
					favorites++
					if mode == "none" {
						paged(`[]`, 0)
					} else if mode == "duplicate" {
						paged(`[{"type":"show","show":{"title":"Synthetic favorite","ids":{"trakt":55}}},{"type":"show","show":{"title":"Duplicate","ids":{"trakt":55}}}]`, 2)
					} else {
						paged(`[{"type":"show","show":{"title":"Synthetic favorite","ids":{"trakt":55}}}]`, 1)
					}
				case "/users/sample/ratings/shows":
					ratings++
					if mode == "none" {
						paged(`[]`, 0)
					} else if mode == "ratingnull" {
						paged(`[{"type":"show","rating":null,"show":{"title":"Invalid rating","ids":{"trakt":55}}}]`, 1)
					} else {
						paged(`[{"type":"show","rating":9,"show":{"title":"Synthetic favorite","ids":{"trakt":55}}},{"type":"show","rating":10,"show":{"title":"Synthetic high rating","ids":{"trakt":56}}},{"type":"show","rating":7,"show":{"title":"Below threshold","ids":{"trakt":57}}}]`, 3)
					}
				case "/shows/55/related", "/shows/56/related":
					if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "6" {
						t.Fatal("unbounded related request", r.URL)
					}
					id := strings.Split(r.URL.Path, "/")[2]
					related = append(related, id)
					if mode == "rate" && len(related) == 2 {
						w.Header().Set("Retry-After", "60")
						w.WriteHeader(429)
						return
					}
					ids := []int{99, 98, 56, 101, 102, 102}
					if id == "56" {
						ids = []int{101, 103}
					}
					rows := []any{}
					for _, id := range ids {
						rows = append(rows, map[string]any{"title": fmt.Sprint("Synthetic ", id), "ids": map[string]int{"trakt": id}})
					}
					_ = json.NewEncoder(w).Encode(rows)
				default:
					t.Fatal("unexpected route", r.URL)
				}
			})
			c.Credentials.RefreshToken = "SYNTHETIC-REFRESH"
			o := SeededRelatedOptions{Type: "show", Source: "both", MinSeedRating: 8, MaxSeeds: 2, PerSeed: 6, Count: 5, ExcludeWatchlisted: true}
			if mode == "count" || mode == "rate" {
				o.Count = 1
			}
			if mode == "budget" {
				o.MaxSeeds = 1
			}
			if mode == "favorites" || mode == "ratings" {
				o.Source = mode
			}
			r, err := c.BuildSeededRelated(o)
			bad := mode == "duplicate" || mode == "ratingnull" || mode == "rate" || mode == "changed"
			if (err != nil) != bad || r.ScanSucceeded == bad {
				t.Fatal(r, err)
			}
			if len(r.Items) > o.Count || len(related) > o.MaxSeeds {
				t.Fatal("budget exceeded", r)
			}
			if mode == "normal" {
				if len(r.Items) != 3 || r.Items[0].TraktID != "101" || len(r.Items[0].RelatedTo) != 2 || r.Seeds[0].TraktID != "55" || !r.Seeds[0].Favorite || *r.Seeds[0].Rating != 9 {
					t.Fatal(r)
				}
				if r.SourceRows != r.CandidatesObserved+r.DuplicatesRemoved+r.ExcludedWatched+r.ExcludedWatchlisted+r.ExcludedSeeds {
					t.Fatal("counter mismatch", r)
				}
			}
			if mode == "count" && (len(r.Items) != 1 || len(r.Items[0].RelatedTo) != 2 || r.CandidatesObserved != 3 || len(related) != 2) {
				t.Fatal("truncated before connections completed", r)
			}
			if mode == "budget" && (len(related) != 1 || r.SeedsAvailable != 2 || r.SeedsRead != 1) {
				t.Fatal(r)
			}
			if mode == "none" && (len(related) != 0 || r.StopReason != "no_eligible_seeds") {
				t.Fatal("silent fallback", r)
			}
			if mode == "rate" && (len(r.Items) != 1 || r.SeedsRead != 1 || r.StopReason != "request_failed") {
				t.Fatal("partial result lost", r)
			}
			if mode == "changed" && r.StopReason != "activity_changed" {
				t.Fatal(r)
			}
			if mode == "favorites" && (ratings != 0 || favorites != 1 || r.SeedsRead != 1) {
				t.Fatal("wrong seed scope", r)
			}
			if mode == "ratings" && (favorites != 0 || ratings != 1 || related[0] != "56") {
				t.Fatal("wrong rating order", r)
			}
			if (mode == "duplicate" || mode == "ratingnull") && len(related) > 0 {
				t.Fatal("invalid seed data used")
			}
		})
	}
}

func TestSeededRelatedValidationBeforeNetwork(t *testing.T) {
	for _, o := range []SeededRelatedOptions{{}, {Type: "person", Source: "both", MinSeedRating: 8, MaxSeeds: 1, PerSeed: 1, Count: 1}, {Type: "show", Source: "both", MinSeedRating: 0, MaxSeeds: 1, PerSeed: 1, Count: 1}, {Type: "movie", Source: "both", MinSeedRating: 8, MaxSeeds: 11, PerSeed: 1, Count: 1}} {
		c := &APIClient{}
		if _, err := c.BuildSeededRelated(o); err == nil {
			t.Fatal("invalid options reached network")
		}
	}
}
