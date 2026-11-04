package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEpisodeQueueSelectionBoundsAndRefusals(t *testing.T) {
	for _, mode := range []string{"normal", "budget", "target", "future", "unknown", "long", "runtime_unknown", "special", "special_allowed", "watched", "completed", "missing_completion", "reset", "identity", "changed", "rate", "bad_history"} {
		t.Run(mode, func(t *testing.T) {
			markers, progress, metadata := 0, 0, 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("write or token refresh", r.Method)
				}
				switch r.URL.Path {
				case "/users/settings":
					fmt.Fprint(w, `{"user":{"ids":{"slug":"sample"}}}`)
				case "/sync/last_activities":
					markers++
					if mode == "changed" && markers == 2 {
						fmt.Fprint(w, `{"all":"changed"}`)
					} else {
						fmt.Fprint(w, `{"all":"same"}`)
					}
				case "/users/sample/history/episodes":
					w.Header().Set("X-Pagination-Page", "1")
					w.Header().Set("X-Pagination-Limit", "1000")
					w.Header().Set("X-Pagination-Page-Count", "1")
					w.Header().Set("X-Pagination-Item-Count", "2")
					date := "2025-01-01T00:00:00Z"
					if mode == "bad_history" {
						date = "not a date"
					}
					firstID := 88
					if mode == "watched" {
						firstID = 5501
					}
					fmt.Fprintf(w, `[{"id":101,"type":"episode","watched_at":%q,"show":{"title":"Synthetic first","ids":{"trakt":55}},"episode":{"season":1,"number":1,"ids":{"trakt":%d}}},{"id":102,"type":"episode","watched_at":"2024-01-01T00:00:00Z","show":{"title":"Synthetic second","ids":{"trakt":56}},"episode":{"season":1,"number":1,"ids":{"trakt":89}}}]`, date, firstID)
				case "/shows/55/progress/watched", "/shows/56/progress/watched":
					progress++
					id := 55
					if strings.Contains(r.URL.Path, "/56/") {
						id = 56
					}
					if mode == "rate" && id == 56 {
						w.Header().Set("Retry-After", "60")
						w.WriteHeader(429)
						return
					}
					if r.URL.Query().Get("hidden") != "false" || r.URL.Query().Get("specials") != fmt.Sprint(mode == "special_allowed") {
						t.Fatal("unbounded specials query", r.URL)
					}
					season := 1
					if mode == "special" || mode == "special_allowed" {
						season = 0
					}
					completed := 1
					if mode == "completed" {
						completed = 2
					}
					flag := "false"
					if mode == "missing_completion" {
						flag = "null"
					}
					reset := "null"
					if mode == "reset" {
						reset = `"2024-01-01T00:00:00Z"`
					}
					fmt.Fprintf(w, `{"aired":2,"completed":%d,"reset_at":%s,"next_episode":{"season":%d,"number":2,"ids":{"trakt":%d}},"seasons":[{"number":%d,"episodes":[{"number":2,"completed":%s}]}]}`, completed, reset, season, id*100+1, season, flag)
				case "/shows/55/seasons/1/episodes/2", "/shows/56/seasons/1/episodes/2", "/shows/55/seasons/0/episodes/2", "/shows/56/seasons/0/episodes/2":
					metadata++
					id := 55
					if strings.Contains(r.URL.Path, "/56/") {
						id = 56
					}
					season := 1
					if mode == "special_allowed" {
						season = 0
					}
					date := `"2020-01-01T00:00:00Z"`
					if mode == "future" {
						date = `"2030-01-01T00:00:00Z"`
					}
					if mode == "unknown" {
						date = "null"
					}
					runtime := "30"
					if mode == "long" {
						runtime = "90"
					}
					if mode == "runtime_unknown" {
						runtime = "null"
					}
					episodeID := id*100 + 1
					if mode == "identity" {
						episodeID++
					}
					fmt.Fprintf(w, `{"title":"Synthetic episode","season":%d,"number":2,"ids":{"trakt":%d},"first_aired":%s,"runtime":%s}`, season, episodeID, date, runtime)
				default:
					t.Fatal("unexpected path", r.URL)
				}
			})
			c.Credentials.RefreshToken = "SYNTHETIC-REFRESH"
			o := EpisodeQueueOptions{Count: 2, MaxShows: 2, RuntimeMax: criteriaInt(45), IncludeSpecials: mode == "special_allowed"}
			if mode == "budget" {
				o.MaxShows = 1
			}
			if mode == "target" {
				o.Count = 1
			}
			r, err := c.buildEpisodeQueueAt(o, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			failed := mode == "identity" || mode == "changed" || mode == "rate" || mode == "bad_history"
			if (err != nil) != failed || r.ScanSucceeded == failed {
				t.Fatal(mode, r, err)
			}
			if mode == "normal" || mode == "special_allowed" {
				if !r.CountMet || len(r.Items) != 2 || r.Items[0].ShowID != "55" || metadata != 2 {
					t.Fatal(r, metadata)
				}
			}
			if mode == "budget" && (r.StopReason != "show_budget" || r.ShowsChecked != 1 || len(r.Items) != 1) {
				t.Fatal(r)
			}
			if mode == "target" && (progress != 1 || r.StopReason != "target_met") {
				t.Fatal(r, progress)
			}
			if mode == "rate" && (len(r.Items) != 1 || r.ScanSucceeded || r.StopReason != "request_failed") {
				t.Fatal("partial state lost", r, err)
			}
			if mode == "changed" && (len(r.Items) != 2 || r.StopReason != "activity_changed") {
				t.Fatal(r)
			}
			if mode == "future" || mode == "unknown" || mode == "long" || mode == "runtime_unknown" || mode == "special" || mode == "completed" || mode == "missing_completion" || mode == "reset" {
				if len(r.Items) != 0 || len(r.Skipped) != 2 {
					t.Fatal("unsafe candidate", r)
				}
			}
			if mode == "watched" && (len(r.Items) != 1 || r.Skipped[0].Reason != "history_progress_disagree") {
				t.Fatal(r)
			}
			encoded, _ := json.Marshal(r)
			if strings.Contains(string(encoded), "SYNTHETIC-REFRESH") {
				t.Fatal("credential leak")
			}
		})
	}
}

func TestEpisodeQueueRejectsInvalidOptionsOffline(t *testing.T) {
	for _, o := range []EpisodeQueueOptions{{}, {Count: 21, MaxShows: 1}, {Count: 1, MaxShows: 21}, {Count: 1, MaxShows: 1, RuntimeMax: criteriaInt(0)}} {
		c := &APIClient{}
		if _, err := c.BuildEpisodeQueue(o); err == nil {
			t.Fatal("invalid options reached credentials/network")
		}
	}
}
