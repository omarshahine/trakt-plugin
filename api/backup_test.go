package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackupResourceRoutes(t *testing.T) {
	for _, tc := range []struct{ resource, list, path string }{{"history", "", "/users/sample/history"}, {"watchlist", "", "/users/sample/watchlist"}, {"lists", "", "/users/sample/lists"}, {"list_items", "7", "/users/sample/lists/7/items"}} {
		t.Run(tc.resource, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer synthetic" || r.Header.Get("Cache-Control") != "no-cache" {
					t.Errorf("bad request %s %s", r.Method, r.URL.Path)
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
	for _, tc := range []struct{ resource, list string }{{"arbitrary", ""}, {"list_items", "../secret"}, {"list_items", "0"}} {
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
