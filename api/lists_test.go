package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCustomListReads(t *testing.T) {
	for _, items := range []bool{false, true} {
		t.Run(fmt.Sprint(items), func(t *testing.T) {
			metadata := `[{"name":"Sample","privacy":"private","description":"Synthetic","sort_by":"rank","sort_how":"asc","ids":{"trakt":123,"slug":"sample"},"future_field":true}]`
			contents := `[{"id":900,"rank":2,"listed_at":"2026-01-01T00:00:00Z","notes":null,"my_rating":8,"type":"movie","movie":{"title":"Synthetic Film","ids":{"trakt":456,"imdb":"tt0000000"}}},{"type":"episode","episode":{"season":1,"number":2}},{"type":"season","season":{"number":1}},{"type":"person","person":{"name":"Synthetic Person"}},{"type":"show","show":{"title":"Synthetic Show"}}]`
			body := metadata
			path := "/users/sample-user/lists"
			if items {
				body = contents
				path += "/sample/items"
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-token" || r.Header.Get("trakt-api-key") != "synthetic-id" {
					t.Error("missing auth")
				}
				if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("limit") != "5" {
					t.Errorf("pagination: %s", r.URL.RawQuery)
				}
				w.Header().Set("X-Pagination-Page", "2")
				w.Header().Set("X-Pagination-Limit", "5")
				w.Header().Set("X-Pagination-Page-Count", "3")
				w.Header().Set("X-Pagination-Item-Count", "12")
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			c := APIClient{Endpoint: srv.URL, Client: srv.Client(), Credentials: Credentials{ClientID: "synthetic-id", AccessToken: "synthetic-token"}}
			var got []json.RawMessage
			var page Pagination
			var err error
			if items {
				got, page, err = c.GetUserListItems("sample-user", "sample", PaginationsParams{2, 5})
			} else {
				got, page, err = c.GetUserLists("sample-user", PaginationsParams{2, 5})
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(got)
			var actual, expected any
			_ = json.Unmarshal(encoded, &actual)
			_ = json.Unmarshal([]byte(body), &expected)
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("records lost fields: %s", encoded)
			}
			if page != (Pagination{"2", "5", "3", "12"}) {
				t.Fatalf("pagination: %+v", page)
			}
		})
	}
}

func TestCustomListErrorsAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"empty", 200, `[]`, ""}, {"malformed", 200, `{`, "decode custom list"},
		{"unauthorized", 401, `sensitive-body`, "401"}, {"private", 403, `sensitive-body`, "403"},
		{"missing", 404, `sensitive-body`, "404"}, {"server", 500, `sensitive-body`, "500"},
		{"rate-limit", 429, `sensitive-body`, "rate limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "17")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := APIClient{Endpoint: srv.URL, Client: srv.Client()}
			got, page, err := c.GetUserListItems("sample", "123", PaginationsParams{1, 10})
			if tc.want == "" {
				if err != nil || got == nil || len(got) != 0 || page != (Pagination{}) {
					t.Fatalf("empty: %v %+v %v", got, page, err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error: %v", err)
				}
				if strings.Contains(err.Error(), "sensitive-body") {
					t.Fatal("error leaked response body")
				}
				if tc.status == 429 {
					var rate *RateLimitError
					if !errors.As(err, &rate) || rate.RetryAfterSeconds != 17 {
						t.Fatalf("lost rate-limit details: %v", err)
					}
				}
			}
		})
	}
}

func TestCustomListValidationAndEscaping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/users/user%2Fsegment/lists/list%3Fquery/items" {
			t.Errorf("path: %s", r.URL.EscapedPath())
		}
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	c := APIClient{Endpoint: srv.URL, Client: srv.Client()}
	if _, _, err := c.GetUserListItems("user/segment", "list?query", PaginationsParams{1, 10}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []PaginationsParams{{0, 10}, {-1, 10}, {1, 0}, {1, 1001}} {
		if _, _, err := c.GetUserLists("user", p); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	if _, _, err := c.GetUserLists(" ", PaginationsParams{1, 10}); err == nil {
		t.Fatal("accepted empty user")
	}
	if _, _, err := c.GetUserListItems("user", " ", PaginationsParams{1, 10}); err == nil {
		t.Fatal("accepted empty list")
	}
}
