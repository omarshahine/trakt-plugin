package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBackupResourceRoutes(t *testing.T) {
	for _, tc := range []struct{ resource, list, path string }{
		{"history", "", "/users/sample/history"}, {"watchlist", "", "/users/sample/watchlist"}, {"lists", "", "/users/sample/lists"}, {"list_items", "7", "/users/sample/lists/7/items"},
		{"ratings/movies", "", "/users/sample/ratings/movies"}, {"ratings/shows", "", "/users/sample/ratings/shows"}, {"ratings/seasons", "", "/users/sample/ratings/seasons"}, {"ratings/episodes", "", "/users/sample/ratings/episodes"},
		{"collection/movies", "", "/users/sample/collection/movies"}, {"collection/shows", "", "/users/sample/collection/shows"}, {"collection/episodes", "", "/users/sample/collection/episodes"}, {"favorites", "", "/users/sample/favorites/media"},
		{"playback", "", "/sync/playback"}, {"watched/movies", "", "/users/sample/watched/movies"}, {"watched/shows", "", "/users/sample/watched/shows"},
		{"hidden/calendar", "", "/users/hidden/calendar"}, {"hidden/progress_watched", "", "/users/hidden/progress_watched"}, {"hidden/progress_watched_reset", "", "/users/hidden/progress_watched_reset"}, {"hidden/progress_collected", "", "/users/hidden/progress_collected"}, {"hidden/recommendations", "", "/users/hidden/recommendations"},
		{"comments/movies", "", "/users/sample/comments/all/movies"}, {"comments/shows", "", "/users/sample/comments/all/shows"}, {"comments/seasons", "", "/users/sample/comments/all/seasons"}, {"comments/episodes", "", "/users/sample/comments/all/episodes"}, {"comments/lists", "", "/users/sample/comments/all/lists"},
		{"notes/movies", "", "/users/sample/notes/movies"}, {"notes/shows", "", "/users/sample/notes/shows"}, {"notes/seasons", "", "/users/sample/notes/seasons"}, {"notes/episodes", "", "/users/sample/notes/episodes"}, {"notes/people", "", "/users/sample/notes/people"}, {"notes/activities", "", "/users/sample/notes/activities"}, {"notes/collection_items", "", "/users/sample/notes/collection_items"}, {"notes/ratings", "", "/users/sample/notes/ratings"},
		{"network/requests", "", "/users/requests"}, {"network/followers", "", "/users/sample/followers"}, {"network/following", "", "/users/sample/following"}, {"network/friends", "", "/users/sample/friends"},
		{"likes/comments", "", "/users/likes/comments"}, {"likes/lists", "", "/users/likes/lists"}, {"saved_filters", "", "/users/saved_filters"}, {"collaborations", "", "/users/sample/lists/collaborations"},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer synthetic" || r.Header.Get("Cache-Control") != "no-cache" {
					t.Errorf("bad request %s %s", r.Method, r.URL.Path)
				}
				if strings.HasPrefix(tc.resource, "collection/") && r.URL.Query().Get("extended") != "metadata" {
					t.Error("collection metadata not requested")
				}
				if strings.HasPrefix(tc.resource, "comments/") && r.URL.Query().Get("include_replies") != "true" {
					t.Error("comment replies omitted")
				}
				if tc.resource == "network/requests" && r.URL.RawQuery != "" {
					t.Error("unpaginated endpoint received paging parameters")
				}
				fmt.Fprint(w, `[{"id":1,"unknown_field":true}]`)
			}))
			defer s.Close()
			c := APIClient{Endpoint: s.URL, Client: s.Client(), Credentials: Credentials{AccessToken: "synthetic"}}
			rows, _, err := c.GetBackupPage("sample", tc.resource, tc.list, PaginationsParams{1, 10})
			if err != nil || len(rows) != 1 {
				t.Fatalf("%v %v", rows, err)
			}
		})
	}
	c := APIClient{}
	for _, tc := range []struct{ resource, list string }{{"arbitrary", ""}, {"list_items", "../secret"}, {"list_items", "0"}, {"ratings/../settings", ""}, {"collection/people", ""}} {
		if _, _, err := c.GetBackupPage("sample", tc.resource, tc.list, PaginationsParams{1, 10}); err == nil {
			t.Fatal("accepted invalid route")
		}
	}
}

func TestBackupActivity(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sync/last_activities" {
			t.Error("wrong route")
		}
		fmt.Fprint(w, `{"all":"synthetic-timestamp"}`)
	}))
	defer s.Close()
	c := APIClient{Endpoint: s.URL, Client: s.Client()}
	if _, err := c.GetBackupActivity(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupDocuments(t *testing.T) {
	for _, tc := range []struct{ resource, path, extended string }{
		{"user/settings", "/users/settings", "browsing,sharing"}, {"user/profile", "/users/sample", "full,vip"}, {"user/stats", "/users/sample/stats", ""},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != tc.path || r.URL.Query().Get("extended") != tc.extended || r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Error("wrong snapshot request")
				}
				fmt.Fprint(w, `{"unknown":{"retained":true}}`)
			}))
			defer s.Close()
			c := APIClient{Endpoint: s.URL, Client: s.Client(), Credentials: Credentials{AccessToken: "synthetic"}}
			d, err := c.GetBackupDocument("sample", tc.resource)
			if err != nil || !strings.Contains(string(d), "retained") {
				t.Fatalf("%s %v", d, err)
			}
		})
	}
	for _, body := range []string{`null`, `[]`, `{}`, `invalid`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c := APIClient{Endpoint: s.URL, Client: s.Client()}
		if _, err := c.GetBackupDocument("sample", "user/settings"); err == nil {
			t.Fatalf("accepted %s", body)
		}
		s.Close()
	}
	c := APIClient{}
	if _, err := c.GetBackupDocument("sample", "../secrets"); err == nil {
		t.Fatal("accepted arbitrary snapshot")
	}
}

func TestBackupEmptyStats204Only(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer s.Close()
	c := APIClient{Endpoint: s.URL, Client: s.Client()}
	data, err := c.GetBackupDocument("sample", "user/stats")
	if err != nil || string(data) != "null" {
		t.Fatalf("stats absence lost: %s %v", data, err)
	}
	if _, err := c.GetBackupDocument("sample", "user/settings"); err == nil {
		t.Fatal("missing account snapshot accepted")
	}
}

func TestBackupRequestsDoesNotGuessCompleteness(t *testing.T) {
	for _, scenario := range []string{"empty", "null", "paginated", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario == "denied" {
					w.WriteHeader(403)
					return
				}
				if scenario == "paginated" {
					w.Header().Set("X-Pagination-Page-Count", "2")
				}
				if scenario == "null" {
					fmt.Fprint(w, `null`)
				} else {
					fmt.Fprint(w, `[]`)
				}
			}))
			defer s.Close()
			c := APIClient{Endpoint: s.URL, Client: s.Client()}
			rows, p, err := c.GetBackupPage("sample", "network/requests", "", PaginationsParams{1, 10})
			if scenario == "empty" {
				if err != nil || len(rows) != 0 || p.ItemCount != "0" {
					t.Fatalf("%+v %v", p, err)
				}
			} else if err == nil {
				t.Fatal("unsafe unpaginated assumption")
			}
		})
	}
}
