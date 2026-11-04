package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPreferenceReadsKeepRawDataAndPagination(t *testing.T) {
	for _, tc := range []struct{ resource, kind, path string }{
		{"ratings", "all", "/users/sample/ratings"}, {"ratings", "movie", "/users/sample/ratings/movies"},
		{"ratings", "show", "/users/sample/ratings/shows"}, {"ratings", "season", "/users/sample/ratings/seasons"},
		{"ratings", "episode", "/users/sample/ratings/episodes"}, {"favorites", "all", "/users/sample/favorites/media"},
		{"favorites", "movie", "/users/sample/favorites/movies"}, {"favorites", "show", "/users/sample/favorites/shows"},
	} {
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != tc.path || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("limit") != "5" || r.Header.Get("Cache-Control") != "no-cache" {
				t.Errorf("wrong read: %s %s", r.Method, r.URL)
			}
			w.Header().Set("X-Pagination-Page", "2")
			w.Header().Set("X-Pagination-Page-Count", "3")
			w.Header().Set("X-Pagination-Limit", "5")
			w.Header().Set("X-Pagination-Item-Count", "12")
			fmt.Fprint(w, `[{"rating":8,"rated_at":"2026-01-01T00:00:00Z","notes":"synthetic","movie":{"ids":{"trakt":9007199254740993}},"future_field":true}]`)
		})
		rows, page, err := c.GetPreferences("sample", tc.resource, tc.kind, PaginationsParams{Page: 2, Limit: 5})
		if err != nil || len(rows) != 1 || page.PageCount != "3" || !strings.Contains(string(rows[0]), "9007199254740993") || !strings.Contains(string(rows[0]), "future_field") {
			t.Fatalf("read lost fields: %+v %+v %v", rows, page, err)
		}
	}
}

func TestPreferenceWritesFollowSyncContracts(t *testing.T) {
	for _, tc := range []struct {
		change         PreferenceChange
		path, response string
		status         int
	}{
		{PreferenceChange{Resource: "ratings", Action: "set", Type: "movie", IDs: []string{"9007199254740993"}, Rating: 8, RatedAt: "2026-01-01T01:00:00+01:00"}, "/sync/ratings", `{"added":{"movies":1},"not_found":{}}`, 201},
		{PreferenceChange{Resource: "ratings", Action: "set", Type: "show", IDs: []string{"55"}, Rating: 9}, "/sync/ratings", `{"added":{"shows":0},"updated":{"shows":1},"not_found":{}}`, 201},
		{PreferenceChange{Resource: "ratings", Action: "set", Type: "season", IDs: []string{"55"}, Rating: 5}, "/sync/ratings", `{"added":{"seasons":1},"not_found":{}}`, 201},
		{PreferenceChange{Resource: "ratings", Action: "remove", Type: "episode", IDs: []string{"55"}}, "/sync/ratings/remove", `{"deleted":{"episodes":1},"not_found":{}}`, 200},
		{PreferenceChange{Resource: "favorites", Action: "add", Type: "movie", IDs: []string{"55", "56"}}, "/sync/favorites", `{"added":{"movies":1},"existing":{"movies":1},"not_found":{}}`, 201},
		{PreferenceChange{Resource: "favorites", Action: "remove", Type: "show", IDs: []string{"55"}}, "/sync/favorites/remove", `{"deleted":{"shows":1},"not_found":{}}`, 200},
	} {
		t.Run(tc.change.Resource+"/"+tc.change.Action+"/"+tc.change.Type, func(t *testing.T) {
			writes := 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if r.URL.Path != "/users/settings" {
						t.Error("wrong account lookup")
					}
					fmt.Fprint(w, `{"user":{"ids":{"slug":"test-account"}}}`)
					return
				}
				writes++
				if r.Method != "POST" || r.URL.Path != tc.path {
					t.Errorf("wrong write %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-token" {
					t.Error("missing auth")
				}
				var payload map[string][]struct {
					IDs struct {
						Trakt json.Number `json:"trakt"`
					} `json:"ids"`
					Rating  int    `json:"rating"`
					RatedAt string `json:"rated_at"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				bucket := tc.change.Type + "s"
				rows := payload[bucket]
				if len(payload) != 1 || len(rows) != len(tc.change.IDs) {
					t.Fatal("wrong payload buckets")
				}
				for i, row := range rows {
					if row.IDs.Trakt.String() != tc.change.IDs[i] || row.Rating != tc.change.Rating {
						t.Error("IDs/rating changed")
					}
					if tc.change.RatedAt != "" && row.RatedAt != "2026-01-01T00:00:00Z" {
						t.Error("not normalized to UTC")
					}
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.response)
			})
			c.Credentials.UserSlug = "stale-personal-account"
			result, err := c.ApplyPreferenceChange("test-account", tc.change)
			if err != nil || result.Outcome != "success" || writes != 1 || result.Account != "test-account" {
				t.Fatalf("bad result: %+v %v writes=%d", result, err, writes)
			}
		})
	}
}

func TestPreferenceAccountMismatchNoWrite(t *testing.T) {
	c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("wrong account was written")
		}
		fmt.Fprint(w, `{"user":{"ids":{"slug":"personal-account"}}}`)
	})
	change := PreferenceChange{Resource: "favorites", Action: "remove", Type: "movie", IDs: []string{"55"}}
	result, err := c.ApplyPreferenceChange("test-account", change)
	if err == nil || result.Outcome != "not_attempted" {
		t.Fatalf("account check bypassed: %+v %v", result, err)
	}
	if _, err := c.ApplyPreferenceChange("", change); err == nil {
		t.Fatal("missing expected account accepted")
	}
}

func TestPreferenceValidationRejectsBeforeRequests(t *testing.T) {
	for _, change := range []PreferenceChange{
		{Resource: "ratings", Action: "set", Type: "movie", IDs: []string{"55"}, Rating: 0},
		{Resource: "ratings", Action: "set", Type: "movie", IDs: []string{"55"}, Rating: 11},
		{Resource: "ratings", Action: "set", Type: "movie", IDs: []string{"55"}, Rating: 8, RatedAt: "unknown"},
		{Resource: "ratings", Action: "set", Type: "movie", IDs: []string{"55", "55"}, Rating: 8},
		{Resource: "ratings", Action: "set", Type: "movie", IDs: []string{"0"}, Rating: 8},
		{Resource: "ratings", Action: "remove", Type: "show", IDs: []string{"55"}, Rating: 5},
		{Resource: "favorites", Action: "add", Type: "season", IDs: []string{"55"}},
		{Resource: "favorites", Action: "add", Type: "movie", IDs: []string{"55"}, RatedAt: "2026-01-01T00:00:00Z"},
		{Resource: "ratings", Action: "add", Type: "movie", IDs: []string{"55"}},
	} {
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("validation contacted server") })
		if _, err := c.ApplyPreferenceChange("test-account", change); err == nil {
			t.Fatal("invalid change accepted")
		}
	}
}

func TestPreferencePartialMalformedAndNoReplay(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{201, `{"added":{"movies":0},"not_found":{"movies":[{"ids":{"trakt":55}}]}}`},
		{201, `{"added":{"movies":0},"not_found":{}}`},
		{201, `{"added":{"movies":1}}`}, {201, `{}`}, {201, `null`}, {201, `invalid`},
		{401, "synthetic-secret-never-echo"}, {403, "denied"}, {420, "limit"}, {429, "limited"}, {500, "failure"},
	} {
		writes := 0
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" {
				fmt.Fprint(w, `{"user":{"ids":{"slug":"test-account"}}}`)
				return
			}
			writes++
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(tc.status)
			fmt.Fprint(w, tc.body)
		})
		result, err := c.ApplyPreferenceChange("test-account", PreferenceChange{Resource: "ratings", Action: "set", Type: "movie", IDs: []string{"55"}, Rating: 8})
		if err == nil || writes != 1 || result.Outcome == "success" || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatalf("unconfirmed write/replay: %+v %v writes=%d", result, err, writes)
		}
	}
}
