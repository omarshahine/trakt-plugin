package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mutationClient(t *testing.T, h http.HandlerFunc) *APIClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &APIClient{Endpoint: srv.URL, Client: srv.Client(), Credentials: Credentials{ClientID: "synthetic-client", AccessToken: "synthetic-token", RefreshToken: "synthetic-refresh"}}
}

func TestListMutationContracts(t *testing.T) {
	notes := "synthetic notes"
	for _, tc := range []struct {
		change                       ListChange
		method, path, body, response string
		status                       int
	}{
		{ListChange{Action: "create", Name: "Synthetic"}, "POST", "/users/sample/lists", `{"name":"Synthetic","description":"","privacy":"private"}`, `{"ids":{"trakt":7},"name":"Synthetic"}`, 201},
		{ListChange{Action: "add", List: "7", Type: "episode", IDs: []string{"9007199254740993"}}, "POST", "/users/sample/lists/7/items", `{"episodes":[{"ids":{"trakt":9007199254740993}}]}`, `{"added":{"episodes":1},"existing":{"episodes":0},"not_found":{}}`, 201},
		{ListChange{Action: "remove", List: "7", Type: "movie", IDs: []string{"55"}}, "POST", "/users/sample/lists/7/items/remove", `{"movies":[{"ids":{"trakt":55}}]}`, `{"deleted":{"movies":1},"not_found":{}}`, 200},
		{ListChange{Action: "notes", List: "7", Item: "900", Notes: &notes}, "PUT", "/users/sample/lists/7/items/900", `{"notes":"synthetic notes"}`, "", 204},
	} {
		t.Run(tc.change.Action, func(t *testing.T) {
			writes := 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/users/settings" {
					fmt.Fprint(w, `{"user":{"vip":true}}`)
					return
				}
				writes++
				if r.Method != tc.method || r.URL.EscapedPath() != tc.path {
					t.Errorf("wrong method/path: %s %s", r.Method, r.URL.EscapedPath())
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-token" {
					t.Error("missing auth")
				}
				var body json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				var actual, want any
				decoder := json.NewDecoder(strings.NewReader(string(body)))
				decoder.UseNumber()
				if err := decoder.Decode(&actual); err != nil {
					t.Error(err)
				}
				decoder = json.NewDecoder(strings.NewReader(tc.body))
				decoder.UseNumber()
				if err := decoder.Decode(&want); err != nil {
					t.Error(err)
				}
				a, err := json.Marshal(actual)
				if err != nil {
					t.Error(err)
				}
				b, err := json.Marshal(want)
				if err != nil {
					t.Error(err)
				}
				if string(a) != string(b) {
					t.Errorf("payload %s != %s", a, b)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.response)
			})
			result, err := c.ApplyListChange("sample", tc.change)
			if err != nil || result.Outcome != "success" || writes != 1 {
				t.Fatalf("result %+v err %v writes %d", result, err, writes)
			}
		})
	}
}

func TestListMutationValidationAndNoReplay(t *testing.T) {
	empty := ""
	long := strings.Repeat("🧭", 501)
	for _, change := range []ListChange{
		{Action: "create", Name: " "}, {Action: "create", Name: "x", Privacy: "everyone"},
		{Action: "add", List: "7", Type: "movie", IDs: []string{"0"}},
		{Action: "add", List: "7", Type: "person", IDs: []string{"55"}},
		{Action: "add", List: "7", Type: "movie", IDs: []string{"55", "55"}},
		{Action: "notes", List: "7", Item: "900"}, {Action: "notes", List: "7", Item: "900", Notes: &long},
		{Action: "notes", List: "7", Item: "0", Notes: &empty},
		{Action: "reorder", List: "7", Rank: []string{"900", "900"}},
	} {
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("validation contacted server") })
		if _, err := c.ApplyListChange("sample", change); err == nil {
			t.Fatal("invalid change accepted", change.Action)
		}
	}
	for _, status := range []int{401, 403, 420, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(status)
				fmt.Fprint(w, "synthetic-secret-must-not-echo")
			})
			_, err := c.ApplyListChange("sample", ListChange{Action: "create", Name: "Synthetic"})
			if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("unexpected retry/leak: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestListNotesFreeAccountNoWrite(t *testing.T) {
	c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/users/settings" {
			t.Error("free account write attempted")
		}
		fmt.Fprint(w, `{"user":{"vip":false}}`)
	})
	notes := "note"
	result, err := c.ApplyListChange("sample", ListChange{Action: "notes", List: "7", Item: "900", Notes: &notes})
	if err == nil || result.Outcome != "not_attempted" || !strings.Contains(err.Error(), "VIP") {
		t.Fatalf("ignored free-account notes: %+v %v", result, err)
	}
}

func TestListMutationPartialAndMalformedResponses(t *testing.T) {
	for _, body := range []string{
		`{"added":{"movies":0},"existing":{"movies":0},"not_found":{"movies":[{"ids":{"trakt":55}}]}}`,
		`{"added":{"movies":0},"existing":{"movies":0},"not_found":{}}`,
		`{}`, `null`, `invalid`,
	} {
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); fmt.Fprint(w, body) })
		result, err := c.ApplyListChange("sample", ListChange{Action: "add", List: "7", Type: "movie", IDs: []string{"55"}})
		if err == nil || result.Outcome == "success" {
			t.Fatalf("bad response accepted: %+v %v", result, err)
		}
	}
}

func TestListReorderPreflightAndRaces(t *testing.T) {
	for _, scenario := range []string{"ok", "changed", "missing", "duplicate", "bad_headers", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			writes, metadata := 0, 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/users/sample/lists/7" {
					metadata++
					stamp := "2026-01-01T00:00:00Z"
					if scenario == "changed" && metadata == 2 {
						stamp = "2026-01-02T00:00:00Z"
					}
					fmt.Fprintf(w, `{"updated_at":%q,"item_count":2}`, stamp)
					return
				}
				if r.Method == "GET" {
					if r.URL.Path != "/users/sample/lists/7/items" || r.Header.Get("Cache-Control") != "no-cache" {
						t.Error("wrong preflight read")
					}
					if scenario != "bad_headers" {
						w.Header().Set("X-Pagination-Page", "1")
						w.Header().Set("X-Pagination-Page-Count", "1")
						w.Header().Set("X-Pagination-Limit", "250")
						w.Header().Set("X-Pagination-Item-Count", "2")
					}
					switch scenario {
					case "missing":
						fmt.Fprint(w, `[{"id":900}]`)
					case "duplicate":
						fmt.Fprint(w, `[{"id":900},{"id":900}]`)
					default:
						fmt.Fprint(w, `[{"id":900},{"id":901}]`)
					}
					return
				}
				writes++
				if r.Method != "POST" || r.URL.Path != "/users/sample/lists/7/items/reorder" {
					t.Error("wrong reorder route")
				}
				var body struct {
					Rank []int `json:"rank"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body.Rank) != 2 || body.Rank[0] != 901 || body.Rank[1] != 900 {
					t.Error("rank used wrong IDs")
				}
				if scenario == "partial" {
					fmt.Fprint(w, `{"updated":1,"skipped_ids":[900]}`)
				} else {
					fmt.Fprint(w, `{"updated":2,"skipped_ids":[]}`)
				}
			})
			result, err := c.ApplyListChange("sample", ListChange{Action: "reorder", List: "7", Rank: []string{"901", "900"}})
			if scenario == "ok" {
				if err != nil || result.Outcome != "success" || writes != 1 {
					t.Fatalf("valid reorder failed: %+v %v", result, err)
				}
			} else if err == nil || (scenario != "partial" && writes != 0) {
				t.Fatalf("unsafe reorder: %+v %v writes=%d", result, err, writes)
			}
		})
	}
}

func TestListMutationEscapingAndClearingNotes(t *testing.T) {
	notes := ""
	p, err := PreviewListChange("user/segment", ListChange{Action: "notes", List: "list?query", Item: "900", Notes: &notes})
	if err != nil || p.Path != "/users/user%2Fsegment/lists/list%3Fquery/items/900" {
		t.Fatalf("unescaped path: %+v %v", p, err)
	}
	body, err := json.Marshal(p.Body)
	if err != nil || string(body) != `{"notes":""}` {
		t.Fatalf("empty notes lost: %s %v", body, err)
	}
}

func TestListMutationFreshAccountResolution(t *testing.T) {
	c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/users/settings" {
			t.Error("unexpected account lookup")
		}
		fmt.Fprint(w, `{"user":{"ids":{"slug":"fresh-account"}}}`)
	})
	c.Credentials.UserSlug = "stale-cached-account"
	slug, err := c.ResolveListOwner()
	if err != nil || slug != "fresh-account" || c.Credentials.UserSlug != "stale-cached-account" {
		t.Fatalf("cached owner trusted or persisted: %s %v", slug, err)
	}
}

func TestListReorderReadsAllPagesBeforeWriting(t *testing.T) {
	pages, writes := 0, 0
	c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/users/sample/lists/7" {
			fmt.Fprint(w, `{"updated_at":"2026-01-01T00:00:00Z","item_count":2}`)
			return
		}
		if r.Method == "GET" {
			pages++
			page := r.URL.Query().Get("page")
			w.Header().Set("X-Pagination-Page", page)
			w.Header().Set("X-Pagination-Page-Count", "2")
			w.Header().Set("X-Pagination-Limit", "1")
			w.Header().Set("X-Pagination-Item-Count", "2")
			if page == "1" {
				fmt.Fprint(w, `[{"id":900}]`)
			} else {
				fmt.Fprint(w, `[{"id":901}]`)
			}
			return
		}
		writes++
		if pages != 2 {
			t.Error("write before all pages")
		}
		fmt.Fprint(w, `{"updated":2,"skipped_ids":[]}`)
	})
	if _, err := c.ApplyListChange("sample", ListChange{Action: "reorder", List: "7", Rank: []string{"901", "900"}}); err != nil || writes != 1 || pages != 2 {
		t.Fatalf("missing pages: %d %d %v", pages, writes, err)
	}
}
