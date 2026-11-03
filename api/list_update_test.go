package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func editString(x string) *string { return &x }
func TestListUpdatePreviewUsesOnlyExplicitFields(t *testing.T) {
	p, err := PreviewListUpdate("me", ListUpdate{List: "55", Description: editString("")})
	if err != nil || p.Method != "PUT" || p.Applied {
		t.Fatal(p, err)
	}
	body := p.Body.(map[string]any)
	if len(body) != 1 || body["description"] != "" {
		t.Fatal("omitted values became resets", body)
	}
	for _, x := range []ListUpdate{{List: "55"}, {List: "slug", Name: editString("new")}, {List: "055", Name: editString("new")}, {List: "55", Name: editString("  ")}, {List: "55", Privacy: editString("invalid")}, {List: "55", Name: editString(strings.Repeat("a", 256))}, {List: "55", Description: editString(strings.Repeat("a", 10001))}} {
		if _, err := PreviewListUpdate("me", x); err == nil {
			t.Fatal("invalid update accepted", x)
		}
	}
}
func TestListUpdateFreshChecksNoopReadbackAndNoReplay(t *testing.T) {
	for _, mode := range []string{"rename", "clear", "privacy", "noop", "wrong_account", "wrong_owner", "wrong_id", "stale", "null_items", "missing_headers", "post401", "post429", "readback_fail", "unrequested_reset", "items_changed", "sort_changed"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			meta := map[string]any{"name": "Original", "description": "Original description", "privacy": "private", "updated_at": "old", "created_at": "created", "item_count": 1, "ids": map[string]int{"trakt": 55}, "user": map[string]any{"ids": map[string]string{"slug": "sample"}}, "sort_by": "rank", "sort_how": "asc", "allow_comments": true, "display_numbers": true, "type": "personal"}
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/users/settings":
					slug := "sample"
					if mode == "wrong_account" {
						slug = "other"
					}
					fmt.Fprintf(w, `{"user":{"ids":{"slug":%q}}}`, slug)
				case r.Method == "GET" && r.URL.Path == "/users/sample/lists/55":
					if mode == "readback_fail" && writes > 0 {
						w.WriteHeader(503)
						return
					}
					response := map[string]any{}
					for k, v := range meta {
						response[k] = v
					}
					if mode == "wrong_owner" {
						response["user"] = map[string]any{"ids": map[string]string{"slug": "other"}}
					}
					if mode == "wrong_id" {
						response["ids"] = map[string]int{"trakt": 56}
					}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				case r.Method == "GET" && r.URL.Path == "/users/sample/lists/55/items":
					w.Header().Set("X-Pagination-Page", "1")
					w.Header().Set("X-Pagination-Page-Count", "1")
					w.Header().Set("X-Pagination-Item-Count", "1")
					w.Header().Set("X-Pagination-Limit", "1000")
					if mode == "missing_headers" {
						w.Header().Del("X-Pagination-Item-Count")
					}
					if mode == "null_items" {
						fmt.Fprint(w, "null")
						return
					}
					rank := 1
					if mode == "items_changed" && writes > 0 {
						rank = 2
					}
					fmt.Fprintf(w, `[{"id":99,"rank":%d,"type":"movie","listed_at":"2020-01-01T00:00:00Z","notes":null,"movie":{"ids":{"trakt":9007199254740993}}}]`, rank)
				case r.Method == "PUT" && r.URL.Path == "/users/sample/lists/55":
					writes++
					if mode == "post401" {
						w.WriteHeader(401)
						fmt.Fprint(w, "SYNTHETIC-PRIVATE")
						return
					}
					if mode == "post429" {
						w.WriteHeader(429)
						return
					}
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if len(body) != 1 {
						t.Fatal("unspecified fields sent", body)
					}
					for k, v := range body {
						meta[k] = v
					}
					meta["updated_at"] = "new"
					if mode == "unrequested_reset" {
						meta["description"] = ""
					}
					if mode == "sort_changed" {
						meta["sort_by"] = "title"
					}
					fmt.Fprint(w, `{}`)
				default:
					t.Errorf("unexpected replay/request %s %s", r.Method, r.URL)
					w.WriteHeader(500)
				}
			})
			c.Credentials.RefreshToken = "SYNTHETIC-REFRESH"
			x := ListUpdate{List: "55", Name: editString("Renamed")}
			if mode == "clear" {
				x.Name = nil
				x.Description = editString("")
			}
			if mode == "privacy" {
				x.Name = nil
				x.Privacy = editString("link")
			}
			if mode == "noop" {
				x.Name = editString("Original")
			}
			if mode == "stale" {
				x.ExpectedUpdatedAt = "not old"
			}
			result, err := c.ApplyListUpdate("sample", x)
			success := mode == "rename" || mode == "clear" || mode == "privacy" || mode == "noop"
			if success {
				if err != nil || !result.Verified || !result.ItemsUnchanged {
					t.Fatal(result, err)
				}
				want := 1
				if mode == "noop" {
					want = 0
				}
				if writes != want || result.Attempted != (want == 1) {
					t.Fatal("unexpected writes", writes, result)
				}
			} else {
				if err == nil || result.Verified {
					t.Fatal("unsafe success", result, err)
				}
				post := strings.HasPrefix(mode, "post") || mode == "readback_fail" || mode == "unrequested_reset" || mode == "items_changed" || mode == "sort_changed"
				want := 0
				if post {
					want = 1
				}
				if writes != want || result.Attempted != post {
					t.Fatal("wrong replay/attempt state", result, writes)
				}
				if strings.Contains(err.Error(), "SYNTHETIC-PRIVATE") {
					t.Fatal("leaked body")
				}
			}
		})
	}
	c := &APIClient{}
	if r, err := c.ApplyListUpdate("", ListUpdate{List: "55", Name: editString("Name")}); err == nil || r.Attempted {
		t.Fatal("missing account reached HTTP")
	}
}
