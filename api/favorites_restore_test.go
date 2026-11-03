package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func writeFavoriteRestorePage(t *testing.T, w http.ResponseWriter, r *http.Request, members map[string]RestoreFavorite, mode string) {
	t.Helper()
	keys := []string{}
	for key := range members {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		t.Fatal("missing page")
	}
	w.Header().Set("X-Pagination-Page", strconv.Itoa(page))
	w.Header().Set("X-Pagination-Limit", "1")
	w.Header().Set("X-Pagination-Page-Count", strconv.Itoa(len(keys)))
	w.Header().Set("X-Pagination-Item-Count", strconv.Itoa(len(keys)))
	if mode == "bad_headers" {
		w.Header().Del("X-Pagination-Item-Count")
	}
	if mode == "null" {
		fmt.Fprint(w, "null")
		return
	}
	rows := []any{}
	if page <= len(keys) {
		index := page - 1
		if mode == "duplicate" && page == 2 {
			index = 0
		}
		x := members[keys[index]]
		if mode == "unsupported" {
			x.Type = "episode"
		}
		rows = append(rows, map[string]any{"type": x.Type, x.Type: map[string]any{"ids": map[string]any{"trakt": json.Number(x.TraktID)}}, "rank": page, "notes": "private metadata never written", "listed_at": "old"})
	}
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		t.Error(err)
	}
}
func TestFavoriteRestoreContractsAndRefusals(t *testing.T) {
	for _, mode := range []string{"success", "empty_account", "already", "noop", "wrong_account", "wrong_uuid", "capacity", "unknown_capacity", "post401", "post429", "post420", "partial", "bad_ack", "race_existing", "other_removed", "extra_added", "readback_fail", "bad_headers", "null", "duplicate", "unsupported", "activity_changed"} {
		t.Run(mode, func(t *testing.T) {
			targets := []RestoreFavorite{{Type: "movie", TraktID: "9007199254740993"}, {Type: "show", TraktID: "55"}}
			members := map[string]RestoreFavorite{"movie:99": {Type: "movie", TraktID: "99"}, "show:99": {Type: "show", TraktID: "99"}}
			if mode == "empty_account" {
				members = map[string]RestoreFavorite{}
			}
			if mode == "already" || mode == "noop" {
				members[favoriteKey(targets[0])] = targets[0]
			}
			if mode == "noop" {
				members[favoriteKey(targets[1])] = targets[1]
			}
			writes, markers := 0, 0
			client := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/users/settings":
					slug, uuid := "sample", "sample-uuid"
					if mode == "wrong_account" {
						slug = "other"
					}
					if mode == "wrong_uuid" {
						uuid = "other-uuid"
					}
					settings := map[string]any{"user": map[string]any{"ids": map[string]string{"slug": slug, "uuid": uuid}}}
					cap := 100
					if mode == "capacity" {
						cap = 3
					}
					if mode != "unknown_capacity" {
						settings["limits"] = map[string]any{"favorites": map[string]int{"item_count": cap}}
					}
					if err := json.NewEncoder(w).Encode(settings); err != nil {
						t.Error(err)
					}
				case r.Method == "GET" && r.URL.Path == "/sync/last_activities":
					markers++
					if mode == "activity_changed" && markers == 2 {
						fmt.Fprint(w, `{"all":"changed"}`)
					} else {
						fmt.Fprint(w, `{"all":"stable"}`)
					}
				case r.Method == "GET" && r.URL.Path == "/users/sample/favorites/media":
					if r.Header.Get("Cache-Control") != "no-cache" {
						t.Error("favorites not revalidated")
					}
					if mode == "readback_fail" && writes > 0 {
						w.WriteHeader(503)
						return
					}
					pageMode := mode
					if writes > 0 {
						pageMode = "success"
					}
					writeFavoriteRestorePage(t, w, r, members, pageMode)
				case r.Method == "POST" && r.URL.Path == "/sync/favorites":
					writes++
					if mode == "post401" {
						w.WriteHeader(401)
						fmt.Fprint(w, "synthetic-secret")
						return
					}
					if mode == "post429" {
						w.Header().Set("Retry-After", "60")
						w.WriteHeader(429)
						return
					}
					if mode == "post420" {
						w.WriteHeader(420)
						return
					}
					var body map[string][]map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if mode == "race_existing" {
						members[favoriteKey(targets[0])] = targets[0]
					}
					added, existing := map[string]int{}, map[string]int{}
					missing := map[string][]any{}
					for bucket, rows := range body {
						if bucket != "movies" && bucket != "shows" {
							t.Fatal("wrong write category", bucket)
						}
						for _, row := range rows {
							if len(row) != 1 || row["ids"] == nil {
								t.Fatal("rank/notes/timestamps or unrelated fields were sent")
							}
							var ids struct {
								Trakt json.Number `json:"trakt"`
							}
							if err := json.Unmarshal(row["ids"], &ids); err != nil {
								t.Fatal(err)
							}
							x := RestoreFavorite{Type: strings.TrimSuffix(bucket, "s"), TraktID: ids.Trakt.String()}
							if mode == "already" && x.Type == "movie" {
								t.Fatal("reposted an existing favorite")
							}
							if mode == "partial" && x.Type == "show" {
								missing[bucket] = append(missing[bucket], map[string]any{"ids": map[string]any{"trakt": 55}})
								continue
							}
							if _, ok := members[favoriteKey(x)]; ok {
								existing[bucket]++
							} else {
								added[bucket]++
							}
							members[favoriteKey(x)] = x
						}
					}
					if mode == "other_removed" {
						delete(members, "movie:99")
					}
					if mode == "extra_added" {
						members["movie:77"] = RestoreFavorite{Type: "movie", TraktID: "77"}
					}
					w.WriteHeader(201)
					if mode == "bad_ack" {
						fmt.Fprint(w, `{"added":null}`)
						return
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"added": added, "existing": existing, "not_found": missing}); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected request or mutation replay: %s %s", r.Method, r.URL)
					w.WriteHeader(500)
				}
			})
			r, err := client.ApplyMissingFavorites("sample", RestoreAccount{Slug: "sample", UUID: "sample-uuid"}, targets)
			success := mode == "success" || mode == "empty_account" || mode == "already" || mode == "noop" || mode == "unknown_capacity" || mode == "race_existing"
			postFailure := strings.HasPrefix(mode, "post") || mode == "partial" || mode == "bad_ack" || mode == "other_removed" || mode == "extra_added" || mode == "readback_fail"
			if success {
				if err != nil || !r.Verified {
					t.Fatalf("membership restore failed: %+v %v", r, err)
				}
				if mode == "noop" {
					if writes != 0 || r.Attempted || r.AlreadyPresent != 2 {
						t.Fatalf("noop wrote: %+v", r)
					}
				} else if writes != 1 || r.Outcome != "verified" {
					t.Fatalf("restore not verified: %+v writes=%d", r, writes)
				}
				if mode == "already" && r.AlreadyPresent != 1 {
					t.Fatal("existing favorite not skipped")
				}
				for _, x := range targets {
					if _, ok := members[favoriteKey(x)]; !ok {
						t.Fatal("target missing")
					}
				}
				prior := writes
				again, e := client.ApplyMissingFavorites("sample", RestoreAccount{Slug: "sample", UUID: "sample-uuid"}, targets)
				if e != nil || !again.Verified || again.Attempted || again.Outcome != "noop" || writes != prior {
					t.Fatalf("rerun mutated membership: %+v %v", again, e)
				}
			} else {
				if err == nil || r.Verified {
					t.Fatalf("unsafe success %+v %v", r, err)
				}
				want := 0
				if postFailure {
					want = 1
				}
				if writes != want || r.Attempted != postFailure {
					t.Fatalf("wrong write state: %+v writes=%d", r, writes)
				}
				if strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatal("leaked response body")
				}
				if mode == "partial" {
					mode = "success"
					resumed, e := client.ApplyMissingFavorites("sample", RestoreAccount{Slug: "sample", UUID: "sample-uuid"}, targets)
					if e != nil || !resumed.Verified || resumed.AlreadyPresent != 1 || writes != 2 {
						t.Fatalf("explicit partial resume failed %+v %v", resumed, e)
					}
				}
			}
		})
	}
}
func TestFavoriteRestoreValidationAndIgnoredMetadata(t *testing.T) {
	client := &APIClient{}
	account := RestoreAccount{Slug: "sample", UUID: "sample-uuid"}
	valid := RestoreFavorite{Type: "movie", TraktID: "55"}
	for _, rows := range [][]RestoreFavorite{{{Type: "episode", TraktID: "55"}}, {{Type: "show", TraktID: "0"}}, {{Type: "movie", TraktID: "055"}}, {valid, valid}, make([]RestoreFavorite, 101)} {
		if r, err := client.ApplyMissingFavorites("sample", account, rows); err == nil || r.Attempted {
			t.Fatal("invalid request reached network")
		}
	}
	if _, err := client.ApplyMissingFavorites("", account, []RestoreFavorite{valid}); err == nil {
		t.Fatal("missing expected account")
	}
	row, err := FavoriteMembership(json.RawMessage(`{"type":"movie","movie":{"ids":{"trakt":9007199254740993}},"rank":5,"notes":"private","listed_at":"ignored"}`))
	if err != nil || row.TraktID != "9007199254740993" {
		t.Fatalf("lost precise membership %+v %v", row, err)
	}
}
