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

func writeRestoreCollectionPage(t *testing.T, w http.ResponseWriter, r *http.Request, collections map[string]RestoreCollection, kind, mode string) {
	t.Helper()
	keys := []string{}
	for key, x := range collections {
		if x.Type == kind {
			keys = append(keys, key)
		}
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
		x := collections[keys[index]]
		rows = append(rows, map[string]any{"metadata": x.Metadata, "collected_at": x.CollectedAt, kind: map[string]any{"ids": map[string]any{"trakt": json.Number(x.TraktID)}}})
	}
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		t.Error(err)
	}
}

func TestCollectionsRestorePreflightWriteReadbackAndNoReplay(t *testing.T) {
	for _, mode := range []string{"success", "already", "noop", "conflict", "date_conflict", "wrong_account", "wrong_uuid", "bad_headers", "null", "duplicate", "activity_changed", "post401", "post429", "partial", "bad_ack", "race_update", "other_changed", "readback_fail", "date_truncated"} {
		t.Run(mode, func(t *testing.T) {
			targets := []RestoreCollection{{Type: "movie", TraktID: "9007199254740993", Metadata: CollectionMetadata{MediaType: "digital"}, CollectedAt: "2020-01-01T00:00:00.123Z"}, {Type: "episode", TraktID: "56", Metadata: CollectionMetadata{MediaType: "digital"}, CollectedAt: "2021-01-01T00:00:00Z"}}
			collections := map[string]RestoreCollection{
				"movie:98": {Type: "movie", TraktID: "98", Metadata: CollectionMetadata{MediaType: "digital"}, CollectedAt: "2018-01-01T00:00:00Z"},
				"movie:99": {Type: "movie", TraktID: "99", Metadata: CollectionMetadata{MediaType: "digital"}, CollectedAt: "2019-01-01T00:00:00Z"},
			}
			if mode == "already" || mode == "noop" || mode == "conflict" || mode == "date_conflict" {
				x := targets[0]
				if mode == "conflict" {
					x.Metadata = CollectionMetadata{}
				}
				if mode == "date_conflict" {
					x.CollectedAt = "2017-01-01T00:00:00Z"
				}
				collections[collectionKey(x)] = x
			}
			if mode == "noop" {
				collections[collectionKey(targets[1])] = targets[1]
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
					if err := json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"ids": map[string]string{"slug": slug, "uuid": uuid}}}); err != nil {
						t.Error(err)
					}
				case r.Method == "GET" && r.URL.Path == "/sync/last_activities":
					markers++
					if mode == "activity_changed" && markers == 2 {
						fmt.Fprint(w, `{"all":"changed"}`)
					} else {
						fmt.Fprint(w, `{"all":"stable"}`)
					}
				case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/users/sample/collection/"):
					if r.URL.Query().Get("extended") != "metadata" {
						t.Fatal("collection technical data not requested")
					}
					if r.Header.Get("Cache-Control") != "no-cache" {
						t.Error("stale collections read")
					}
					if mode == "readback_fail" && writes > 0 {
						w.WriteHeader(503)
						return
					}
					pageMode := mode
					if writes > 0 {
						pageMode = "success"
					}
					kind := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/sample/collection/"), "s")
					writeRestoreCollectionPage(t, w, r, collections, kind, pageMode)
				case r.Method == "POST" && r.URL.Path == "/sync/collection":
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
					var body map[string][]struct {
						IDs struct {
							Trakt json.Number `json:"trakt"`
						} `json:"ids"`
						MediaType   string `json:"media_type"`
						CollectedAt string `json:"collected_at"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if mode == "race_update" {
						x := targets[0]
						x.Metadata = CollectionMetadata{}
						collections[collectionKey(x)] = x
					}
					added, updated := map[string]int{}, map[string]int{}
					missing := map[string][]any{}
					for bucket, items := range body {
						if bucket != "movies" && bucket != "episodes" && !(mode == "all_types" && (bucket == "shows" || bucket == "seasons")) {
							t.Fatal("unexpected category written", bucket)
						}
						for _, item := range items {
							x := RestoreCollection{Type: strings.TrimSuffix(bucket, "s"), TraktID: item.IDs.Trakt.String(), Metadata: CollectionMetadata{MediaType: item.MediaType}, CollectedAt: item.CollectedAt}
							if mode == "already" && x.Type == "movie" {
								t.Fatal("rewrote an existing collection")
							}
							if mode == "partial" && x.Type == "episode" {
								missing[bucket] = append(missing[bucket], map[string]any{"ids": map[string]any{"trakt": 56}})
								continue
							}
							if _, ok := collections[collectionKey(x)]; ok {
								updated[bucket]++
							} else {
								added[bucket]++
							}
							if mode == "date_truncated" {
								x.CollectedAt = strings.ReplaceAll(x.CollectedAt, ".123", "")
							}
							collections[collectionKey(x)] = x
						}
					}
					if mode == "other_changed" {
						x := collections["movie:99"]
						x.Metadata = CollectionMetadata{}
						collections["movie:99"] = x
					}
					w.WriteHeader(201)
					if mode == "bad_ack" {
						fmt.Fprint(w, `{"added":null}`)
						return
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"added": added, "updated": updated, "not_found": missing}); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected request / mutation replay: %s %s", r.Method, r.URL)
					w.WriteHeader(500)
				}
			})
			result, err := client.ApplyMissingCollections("sample", RestoreAccount{Slug: "sample", UUID: "sample-uuid"}, targets)
			success := mode == "success" || mode == "already" || mode == "noop"
			postFailure := strings.HasPrefix(mode, "post") || mode == "partial" || mode == "bad_ack" || mode == "race_update" || mode == "other_changed" || mode == "readback_fail" || mode == "date_truncated"
			if success {
				if err != nil || !result.Verified {
					t.Fatalf("restore failed %+v %v", result, err)
				}
				if mode == "noop" {
					if writes != 0 || result.AlreadyPresent != 2 || result.Outcome != "noop" {
						t.Fatalf("noop wrote: %+v %d", result, writes)
					}
				} else if writes != 1 || result.Outcome != "verified" {
					t.Fatalf("not verified: %+v %d", result, writes)
				}
				if mode == "already" && result.AlreadyPresent != 1 {
					t.Fatal("existing collection not skipped")
				}
				for _, x := range targets {
					if collections[collectionKey(x)] != x {
						t.Fatal("value/date not restored")
					}
				}
				priorWrites := writes
				again, e := client.ApplyMissingCollections("sample", RestoreAccount{Slug: "sample", UUID: "sample-uuid"}, targets)
				if e != nil || !again.Verified || again.Attempted || writes != priorWrites || again.Outcome != "noop" {
					t.Fatalf("rerun rewrote collections: %+v %v", again, e)
				}
			} else {
				if err == nil || result.Verified {
					t.Fatalf("unsafe success: %+v %v", result, err)
				}
				wantWrites := 0
				if postFailure {
					wantWrites = 1
				}
				if writes != wantWrites || result.Attempted != postFailure {
					t.Fatalf("wrong write/replay state: %+v writes=%d want=%d", result, writes, wantWrites)
				}
				if strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatal("leaked error body")
				}
				if mode == "partial" {
					// A separate, explicit apply after inspection may finish missing
					// rows, but must skip the already completed movie collection.
					mode = "success"
					resumed, resumeErr := client.ApplyMissingCollections("sample", RestoreAccount{Slug: "sample", UUID: "sample-uuid"}, targets)
					if resumeErr != nil || !resumed.Verified || resumed.AlreadyPresent != 1 || writes != 2 {
						t.Fatalf("explicit partial resume failed or rewrote completed rows: %+v %v writes=%d", resumed, resumeErr, writes)
					}
				}
			}
		})
	}
}

func TestCollectionRestoreRejectsAmbiguousTechnicalValues(t *testing.T) {
	for _, m := range []string{`{"3d":false}`, `{"3d":1}`, `{"hdr":"hdr10"}`, `{"resolution":"uhd_4k"}`, `{"unknown":null}`, `[]`} {
		row := `{"movie":{"ids":{"trakt":55}},"collected_at":"2020-01-01T00:00:00Z","metadata":` + m + `}`
		if _, err := DecodeRestoreCollection(json.RawMessage(row), "movie"); err == nil {
			t.Fatal("unsupported metadata accepted", m)
		}
	}
	row := `{"movie":{"ids":{"trakt":9007199254740993}},"collected_at":"2020-01-01T01:00:00+01:00","metadata":{"3d":null,"hdr":null,"media_type":"digital"}}`
	x, err := DecodeRestoreCollection(json.RawMessage(row), "movie")
	if err != nil || x.TraktID != "9007199254740993" || x.CollectedAt != "2020-01-01T00:00:00Z" || x.Metadata.MediaType != "digital" {
		t.Fatal(x, err)
	}
	client := &APIClient{}
	account := RestoreAccount{Slug: "sample", UUID: "uuid"}
	for _, items := range [][]RestoreCollection{{{Type: "show", TraktID: "55", CollectedAt: x.CollectedAt}}, {{Type: "movie", TraktID: "055", CollectedAt: x.CollectedAt}}, {{Type: "movie", TraktID: "55"}}, {x, x}, make([]RestoreCollection, 101)} {
		if r, err := client.ApplyMissingCollections("sample", account, items); err == nil || r.Attempted {
			t.Fatal("invalid batch reached HTTP")
		}
	}
}
