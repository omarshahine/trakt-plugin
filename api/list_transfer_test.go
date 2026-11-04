package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type transferFixture struct {
	ID, Media, Rank int
	Note            string
}

func TestTransferDestinationBeforeRemovalAndPartialStops(t *testing.T) {
	for _, mode := range []string{"copy", "move", "dest_existing", "already_moved", "wrong_account", "missing_source", "duplicate_rank", "notes", "add429", "bad_add_ack", "lost_dest", "source_changed", "dest_changed", "remove401", "remove_partial", "kept_item_changed"} {
		t.Run(mode, func(t *testing.T) {
			src := []transferFixture{{101, 55, 1, ""}, {102, 56, 2, ""}}
			dst := []transferFixture{{201, 57, 1, ""}}
			if mode == "dest_existing" || mode == "already_moved" {
				dst = append(dst, transferFixture{202, 55, 2, ""})
			}
			if mode == "already_moved" || mode == "missing_source" {
				src = src[1:]
			}
			if mode == "duplicate_rank" {
				src[1].Rank = 1
			}
			if mode == "notes" {
				src[0].Note = "source note must not be lost"
			}
			addWrites, removeWrites, destReads := 0, 0, 0
			history := []string{}
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				history = append(history, r.Method+" "+r.URL.Path)
				if r.URL.Path == "/users/settings" {
					slug := "sample"
					if mode == "wrong_account" {
						slug = "other"
					}
					fmt.Fprintf(w, `{"user":{"ids":{"slug":%q}}}`, slug)
					return
				}
				parts := strings.Split(r.URL.Path, "/")
				if len(parts) < 5 || parts[1] != "users" || parts[2] != "sample" {
					t.Errorf("unexpected request/replay %s %s", r.Method, r.URL)
					w.WriteHeader(500)
					return
				}
				id := parts[4]
				var rows *[]transferFixture
				if id == "11" {
					rows = &src
				} else if id == "22" {
					rows = &dst
				} else {
					t.Fatal("wrong list", id)
				}
				if r.Method == "GET" && len(parts) == 5 {
					if err := json.NewEncoder(w).Encode(map[string]any{"ids": map[string]int{"trakt": mustTransferInt(id)}, "user": map[string]any{"ids": map[string]string{"slug": "sample"}}, "name": "List " + id, "description": "keep", "privacy": "private", "item_count": len(*rows), "updated_at": "revision", "sort_by": "rank"}); err != nil {
						t.Error(err)
					}
					return
				}
				if r.Method == "GET" && len(parts) == 6 && parts[5] == "items" {
					if id == "22" {
						destReads++
						if mode == "lost_dest" && addWrites > 0 {
							*rows = (*rows)[:1]
						}
						if mode == "dest_changed" && destReads == 3 {
							(*rows)[0].Note = "changed after copy verification"
						}
					}
					if mode == "source_changed" && id == "11" && addWrites > 0 {
						(*rows)[0].Note = "concurrent source note"
					}
					if mode == "kept_item_changed" && id == "11" && removeWrites > 0 {
						(*rows)[0].Note = "unexpected surviving note"
					}
					w.Header().Set("X-Pagination-Page", "1")
					w.Header().Set("X-Pagination-Limit", "1000")
					w.Header().Set("X-Pagination-Page-Count", "1")
					w.Header().Set("X-Pagination-Item-Count", strconv.Itoa(len(*rows)))
					out := []any{}
					for _, e := range *rows {
						out = append(out, map[string]any{"id": e.ID, "rank": e.Rank, "type": "movie", "listed_at": "2020-01-01T00:00:00Z", "notes": e.Note, "movie": map[string]any{"ids": map[string]int{"trakt": e.Media}}})
					}
					if err := json.NewEncoder(w).Encode(out); err != nil {
						t.Error(err)
					}
					return
				}
				if r.Method == "POST" && id == "22" && strings.HasSuffix(r.URL.Path, "/items") {
					addWrites++
					if mode == "add429" {
						w.WriteHeader(429)
						return
					}
					var body map[string][]struct {
						IDs struct {
							Trakt int `json:"trakt"`
						} `json:"ids"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || len(body["movies"]) != 1 || body["movies"][0].IDs.Trakt != 55 {
						t.Fatal("wrong add payload")
					}
					dst = append(dst, transferFixture{202, 55, 2, ""})
					w.WriteHeader(201)
					if mode == "bad_add_ack" {
						fmt.Fprint(w, `{}`)
					} else {
						fmt.Fprint(w, `{"added":{"movies":1},"existing":{"movies":0},"not_found":{"movies":[]}}`)
					}
					return
				}
				if r.Method == "POST" && id == "11" && strings.HasSuffix(r.URL.Path, "/items/remove") {
					removeWrites++
					if destReads < 3 {
						t.Error("source removal before two destination verifications")
					}
					found := false
					for _, e := range dst {
						if e.Media == 55 {
							found = true
						}
					}
					if !found {
						t.Fatal("source deletion without destination membership")
					}
					if mode == "remove401" {
						w.WriteHeader(401)
						return
					}
					src = src[1:]
					src[0].Rank = 1
					if mode == "remove_partial" {
						fmt.Fprint(w, `{"deleted":{"movies":0},"not_found":{"movies":[]}}`)
					} else {
						fmt.Fprint(w, `{"deleted":{"movies":1},"not_found":{"movies":[]}}`)
					}
					return
				}
				t.Errorf("unexpected route %s %s", r.Method, r.URL)
				w.WriteHeader(500)
			})
			c.Credentials.RefreshToken = "SYNTHETIC-REFRESH"
			action := "move"
			if mode == "copy" {
				action = "copy"
			}
			r, err := c.ApplyListTransfer("sample", ListTransfer{Action: action, Source: "11", Destination: "22", Type: "movie", IDs: []string{"55"}})
			good := mode == "copy" || mode == "move" || mode == "dest_existing" || mode == "already_moved"
			if good {
				if err != nil || !r.Verified || !r.DestinationVerified {
					t.Fatalf("failed transfer %+v %v %v", r, err, history)
				}
				if mode == "copy" && (removeWrites != 0 || len(src) != 2) {
					t.Fatal("copy changed source")
				}
				if mode == "move" || mode == "dest_existing" {
					if removeWrites != 1 || len(src) != 1 {
						t.Fatal("move did not remove exactly target")
					}
				}
				if mode == "dest_existing" && addWrites != 0 {
					t.Fatal("rewrote existing destination")
				}
				if mode == "already_moved" && (r.Attempted || r.Outcome != "noop") {
					t.Fatal("replayed completed move")
				}
			} else {
				if err == nil || r.Verified {
					t.Fatal("unsafe success", r, err)
				}
				if mode != "remove401" && mode != "remove_partial" && mode != "kept_item_changed" && removeWrites != 0 {
					t.Fatal("unsafe source deletion", mode, r, history)
				}
			}
			if addWrites > 1 || removeWrites > 1 {
				t.Fatal("mutation replay")
			}
		})
	}
}
func mustTransferInt(s string) int { n, _ := strconv.Atoi(s); return n }
func TestTransferPreviewValidation(t *testing.T) {
	valid := ListTransfer{Action: "move", Source: "11", Destination: "22", Type: "episode", IDs: []string{"9007199254740993"}}
	if p, err := PreviewListTransfer(valid); err != nil || p.Applied || !p.ReadOnly {
		t.Fatal(p, err)
	}
	for _, x := range []ListTransfer{{Action: "move", Source: "11", Destination: "11", Type: "movie", IDs: []string{"55"}}, {Action: "copy", Source: "slug", Destination: "22", Type: "movie", IDs: []string{"55"}}, {Action: "move", Source: "11", Destination: "22", Type: "person", IDs: []string{"55"}}, {Action: "move", Source: "11", Destination: "22", Type: "movie", IDs: []string{"55", "55"}}, {Action: "move", Source: "11", Destination: "22", Type: "movie", IDs: make([]string, 101)}} {
		if _, err := PreviewListTransfer(x); err == nil {
			t.Fatal("unsafe transfer accepted")
		}
	}
}
