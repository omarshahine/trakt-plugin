package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestDiscoveryRoutesFormatsPaginationAndReadOnly(t *testing.T) {
	for _, kind := range []string{"movie", "show"} {
		for _, mode := range []string{"trending", "popular", "recommendations", "related"} {
			t.Run(kind+"_"+mode, func(t *testing.T) {
				calls := 0
				c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					want := "/" + kind + "s/" + mode
					if mode == "related" {
						want = "/" + kind + "s/55/related"
					}
					if mode == "recommendations" {
						want = "/recommendations/" + kind + "s"
					}
					if r.Method != "GET" || r.URL.Path != want || r.URL.Query().Get("extended") != "full" || r.URL.Query().Get("limit") != "2" {
						t.Errorf("unexpected discovery request %s %s", r.Method, r.URL)
					}
					if mode == "recommendations" {
						if r.URL.Query().Get("page") != "" {
							t.Error("fabricated recommendations pagination")
						}
					} else {
						if r.URL.Query().Get("page") != "3" {
							t.Error("page lost")
						}
						w.Header().Set("X-Pagination-Page", "3")
						w.Header().Set("X-Pagination-Page-Count", "4")
						w.Header().Set("X-Pagination-Item-Count", "8")
						w.Header().Set("X-Pagination-Limit", "2")
					}
					media := json.RawMessage(`{"title":"Synthetic title","year":null,"ids":{"trakt":9007199254740993},"overview":"preserved"}`)
					var rows []any
					if mode == "trending" {
						rows = []any{map[string]any{"watchers": 7, kind: media}}
					} else {
						rows = []any{media}
					}
					if err := json.NewEncoder(w).Encode(rows); err != nil {
						t.Error(err)
					}
				})
				page := 3
				id := ""
				if mode == "recommendations" {
					page = 1
				}
				if mode == "related" {
					id = "55"
				}
				r, err := c.Discover(DiscoveryOptions{Mode: mode, Type: kind, ID: id, Page: page, Limit: 2})
				if err != nil || calls != 1 || len(r.Items) != 1 || r.Items[0].TraktID != "9007199254740993" || r.Items[0].Year != nil || r.WatchedFiltered || r.PaginationAvailable != (mode != "recommendations") {
					t.Fatalf("bad result %+v %v calls=%d", r, err, calls)
				}
				if mode == "trending" && (r.Items[0].Watchers == nil || *r.Items[0].Watchers != 7) {
					t.Fatal("watchers lost")
				}
			})
		}
	}
}
func TestDiscoveryRefusesInvalidInputsBeforeNetwork(t *testing.T) {
	c := &APIClient{}
	for _, o := range []DiscoveryOptions{{Mode: "bad", Type: "movie", Page: 1, Limit: 10}, {Mode: "popular", Type: "person", Page: 1, Limit: 10}, {Mode: "related", Type: "show", ID: "../settings", Page: 1, Limit: 10}, {Mode: "related", Type: "show", ID: "055", Page: 1, Limit: 10}, {Mode: "popular", Type: "show", ID: "55", Page: 1, Limit: 10}, {Mode: "recommendations", Type: "show", Page: 2, Limit: 10}, {Mode: "trending", Type: "show", Page: 0, Limit: 10}, {Mode: "popular", Type: "show", Page: 1, Limit: 101}} {
		if _, err := c.Discover(o); err == nil {
			t.Fatal("invalid request reached network", o)
		}
	}
}
func TestDiscoveryEmptyMalformedAndFailuresNoReplay(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		ok     bool
	}{{"[]", 200, true}, {"null", 200, false}, {"{}", 200, false}, {`[{"title":"missing id"}]`, 200, false}, {`[{"title":"wrong id","ids":{"trakt":1.5}}]`, 200, false}, {`[{"title":"a","ids":{"trakt":55}},{"title":"b","ids":{"trakt":55}}]`, 200, true}, {"SYNTHETIC-PRIVATE-ERROR", 401, false}, {"SYNTHETIC-PRIVATE-ERROR", 403, false}, {"SYNTHETIC-PRIVATE-ERROR", 429, false}} {
		calls := 0
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != "GET" {
				t.Error("unexpected write/refresh")
			}
			w.WriteHeader(tc.status)
			fmt.Fprint(w, tc.body)
		})
		r, err := c.Discover(DiscoveryOptions{Mode: "popular", Type: "movie", Page: 1, Limit: 10})
		if (err == nil) != tc.ok || calls != 1 {
			t.Fatalf("unexpected outcome %+v %v calls %d", r, err, calls)
		}
		if tc.ok && r.Items == nil {
			t.Fatal("empty response became null")
		}
	}
}
