package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func eventTarget(kind string) HistoryEventRef {
	return HistoryEventRef{HistoryID: "9007199254740993", Type: kind, TraktID: "55", WatchedAt: "2020-01-01T00:00:00.123Z", Action: "watch"}
}

func writeEventPage(t *testing.T, w http.ResponseWriter, r *http.Request, events []HistoryEventRef, mode string) {
	t.Helper()
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		t.Error("missing page")
	}
	limit := 1 // Exercise a server cap smaller than the requested page size.
	pages := (len(events) + limit - 1) / limit
	total := len(events)
	if mode == "bad_count" {
		total++
	}
	if mode == "unstable_pages" && page == 2 {
		total++
	}
	w.Header().Set("X-Pagination-Page", strconv.Itoa(page))
	w.Header().Set("X-Pagination-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Pagination-Page-Count", strconv.Itoa(pages))
	w.Header().Set("X-Pagination-Item-Count", strconv.Itoa(total))
	if mode == "missing_headers" {
		w.Header().Del("X-Pagination-Item-Count")
	}
	if mode == "null" {
		fmt.Fprint(w, "null")
		return
	}
	rows := []map[string]any{}
	if page <= len(events) {
		e := events[page-1]
		if mode == "duplicate" && page == 2 {
			e.HistoryID = events[0].HistoryID
		}
		if mode == "wrong_media" {
			e.TraktID = "99"
		}
		if mode == "invalid_date" {
			e.WatchedAt = "unknown"
		}
		if mode == "wrong_type" {
			e.Type = "show"
		}
		rows = append(rows, map[string]any{"id": json.Number(e.HistoryID), "type": e.Type, "watched_at": e.WatchedAt, "action": e.Action, e.Type: map[string]any{"ids": map[string]any{"trakt": json.Number(e.TraktID)}}})
	}
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		t.Error(err)
	}
}

func TestHistoryEventRemovalContractsAndRefusals(t *testing.T) {
	for _, kind := range []string{"movie", "episode"} {
		for _, mode := range []string{"success", "last_event", "wrong_account", "absent", "wrong_date", "bad_count", "unstable_pages", "missing_headers", "null", "duplicate", "wrong_media", "wrong_type", "invalid_date", "activity_changed", "post_401", "post_429", "bad_ack", "target_remains", "other_disappeared", "other_changed", "readback_failed"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				target := eventTarget(kind)
				other := target
				other.HistoryID = "9007199254740994"
				other.WatchedAt = "2021-01-01T00:00:00Z"
				events := []HistoryEventRef{target, other}
				if mode == "last_event" {
					events = events[:1]
				}
				writes, markers := 0, 0
				client := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == "GET" && r.URL.Path == "/users/settings":
						fmt.Fprint(w, `{"user":{"ids":{"slug":"sample"}}}`)
					case r.Method == "GET" && r.URL.Path == "/sync/last_activities":
						markers++
						if mode == "activity_changed" && markers == 2 {
							fmt.Fprint(w, `{"all":"changed"}`)
						} else {
							fmt.Fprint(w, `{"all":"stable"}`)
						}
					case r.Method == "GET" && r.URL.Path == "/users/sample/history/"+kind+"s/55":
						if r.Header.Get("Cache-Control") != "no-cache" {
							t.Error("history read not revalidated")
						}
						if mode == "readback_failed" && writes > 0 {
							w.WriteHeader(503)
							return
						}
						pageMode := mode
						if writes > 0 {
							pageMode = "success"
						}
						writeEventPage(t, w, r, events, pageMode)
					case r.Method == "POST" && r.URL.Path == "/sync/history/remove":
						writes++
						data, err := io.ReadAll(r.Body)
						if err != nil {
							t.Fatal(err)
						}
						if string(data) != `{"ids":[9007199254740993]}` {
							t.Errorf("unsafe media-wide payload or rounded ID: %s", data)
						}
						if mode == "post_401" {
							w.WriteHeader(401)
							fmt.Fprint(w, "synthetic-secret-not-for-output")
							return
						}
						if mode == "post_429" {
							w.Header().Set("Retry-After", "60")
							w.WriteHeader(429)
							return
						}
						if mode != "target_remains" {
							events = events[1:]
						}
						if mode == "other_disappeared" {
							events = nil
						}
						if mode == "other_changed" {
							events[0].WatchedAt = "2022-01-01T00:00:00Z"
						}
						if mode == "bad_ack" {
							fmt.Fprint(w, `{"deleted":{"movies":0,"episodes":0}}`)
							return
						}
						bucket := kind + "s"
						if err := json.NewEncoder(w).Encode(map[string]any{"deleted": map[string]int{bucket: 1}, "not_found": map[string]any{"ids": []any{}}}); err != nil {
							t.Error(err)
						}
					default:
						t.Errorf("unexpected request or replay: %s %s", r.Method, r.URL)
						w.WriteHeader(500)
					}
				})
				expected := "sample"
				if mode == "wrong_account" {
					expected = "other"
				}
				requested := target
				if mode == "wrong_date" {
					requested.WatchedAt = "2019-01-01T00:00:00Z"
				}
				if mode == "absent" {
					requested.HistoryID = "42"
				}
				result, err := client.ApplyHistoryEventRemoval(expected, requested)
				postFailure := strings.HasPrefix(mode, "post_") || mode == "bad_ack" || mode == "target_remains" || strings.HasPrefix(mode, "other_") || mode == "readback_failed"
				if mode == "last_event" {
					if err != nil || !result.Verified || writes != 1 || result.BeforeCount != 1 || result.AfterCount != 0 || len(events) != 0 {
						t.Fatalf("empty scoped history was not verified: %+v %v", result, err)
					}
				} else if mode == "success" {
					if err != nil || !result.Verified || result.Outcome != "verified" || writes != 1 || result.BeforeCount != 2 || result.AfterCount != 1 || len(events) != 1 || events[0] != other {
						t.Fatalf("failed exact deletion: %+v %v writes=%d", result, err, writes)
					}
				} else {
					if err == nil || result.Verified {
						t.Fatalf("unsafe success: %+v %v", result, err)
					}
					wantWrites := 0
					if postFailure {
						wantWrites = 1
					}
					if writes != wantWrites {
						t.Fatalf("write/replay count %d want %d", writes, wantWrites)
					}
					if result.Attempted != postFailure {
						t.Fatalf("incorrect attempted state: %+v", result)
					}
					if strings.Contains(err.Error(), "synthetic-secret") {
						t.Fatal("leaked rejected response body")
					}
				}
				if mode == "success" || mode == "last_event" {
					again, repeatErr := client.ApplyHistoryEventRemoval(expected, requested)
					if repeatErr == nil || again.Attempted || writes != 1 {
						t.Fatalf("already removed event was replayed: %+v %v writes=%d", again, repeatErr, writes)
					}
				}
			})
		}
	}
}

func TestHistoryEventRemovalOfflineValidation(t *testing.T) {
	valid := eventTarget("movie")
	for _, tc := range []struct{ field, value string }{
		{"history_id", "0"}, {"history_id", "01"}, {"history_id", "1e2"}, {"history_id", "9223372036854775808"},
		{"history_id", "55,56"}, {"history_id", "55;command"}, {"trakt_id", "-1"}, {"trakt_id", "1.1"},
		{"type", "show"}, {"type", "season"}, {"watched_at", "unknown"}, {"watched_at", "2020-01-01"},
	} {
		target := valid
		switch tc.field {
		case "history_id":
			target.HistoryID = tc.value
		case "trakt_id":
			target.TraktID = tc.value
		case "type":
			target.Type = tc.value
		case "watched_at":
			target.WatchedAt = tc.value
		}
		if _, err := PreviewHistoryEventRemoval(target); err == nil {
			t.Fatalf("invalid target accepted: %+v", target)
		}
	}
	valid.WatchedAt = "2020-01-01T01:00:00.123+01:00"
	preview, err := PreviewHistoryEventRemoval(valid)
	if err != nil || preview.Target.WatchedAt != "2020-01-01T00:00:00.123Z" || preview.Applied {
		t.Fatalf("bad offline preview: %+v %v", preview, err)
	}
	client := &APIClient{} // Must fail before HTTP or credential lookup.
	result, err := client.ApplyHistoryEventRemoval("", valid)
	if err == nil || result.Attempted {
		t.Fatal("missing account reached mutation")
	}
}
