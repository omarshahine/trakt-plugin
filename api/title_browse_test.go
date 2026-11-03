package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func browseInt(n int) *int { return &n }
func TestTitleBrowseRoutesMetadataAndSpecials(t *testing.T) {
	for _, tc := range []struct {
		o          TitleBrowseOptions
		path, body string
		count      int
	}{
		{TitleBrowseOptions{Mode: "details", Type: "movie", ID: "9007199254740993"}, "/movies/9007199254740993", `{"title":"Synthetic movie","ids":{"trakt":9007199254740993},"overview":"kept","runtime":123,"rating":7.5}`, 1},
		{TitleBrowseOptions{Mode: "details", Type: "show", ID: "55"}, "/shows/55", `{"title":"Synthetic show","ids":{"trakt":55},"status":"ended"}`, 1},
		{TitleBrowseOptions{Mode: "seasons", Type: "show", ID: "55"}, "/shows/55/seasons", `[{"number":0,"ids":{"trakt":99}},{"number":1,"ids":{"trakt":100}}]`, 2},
		{TitleBrowseOptions{Mode: "episodes", Type: "show", ID: "55", Season: browseInt(0)}, "/shows/55/seasons/0", `[{"number":0,"season":0,"ids":{"trakt":9007199254740993},"first_aired":"2020-01-01T00:00:00Z","overview":"special"}]`, 1},
		{TitleBrowseOptions{Mode: "episode", Type: "show", ID: "55", Season: browseInt(1), Episode: browseInt(2)}, "/shows/55/seasons/1/episodes/2", `{"number":2,"season":1,"ids":{"trakt":88},"first_aired":null}`, 1},
		{TitleBrowseOptions{Mode: "episodes", Type: "show", ID: "55", Season: browseInt(1)}, "/shows/55/seasons/1", `[]`, 0},
	} {
		t.Run(tc.path, func(t *testing.T) {
			calls := 0
			c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != tc.path || r.URL.Query().Get("extended") != "full" {
					t.Fatal("wrong metadata route", r.Method, r.URL)
				}
				fmt.Fprint(w, tc.body)
			})
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			r, err := c.browseTitleAt(tc.o, now)
			if err != nil || calls != 1 || len(r.Items) != tc.count || r.AsOf != "2026-01-01T00:00:00Z" {
				t.Fatal(r, err, calls)
			}
			if len(r.Items) > 0 && !strings.Contains(string(r.Items[0].Media), "ids") {
				t.Fatal("raw metadata lost")
			}
			if tc.o.Mode == "episode" && r.Items[0].AirStatus != "unknown" {
				t.Fatal("null date became upcoming")
			}
			if tc.o.Mode == "episodes" && tc.count > 0 && (r.Items[0].TraktID != "9007199254740993" || *r.Items[0].Season != 0 || r.Items[0].AirStatus != "aired") {
				t.Fatal("specials or precision lost", r)
			}
		})
	}
}
func TestEpisodeAirStatusBoundariesAndUnknown(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ raw, want string }{{`"2026-01-01T11:59:59Z"`, "aired"}, {`"2026-01-01T12:00:00Z"`, "aired"}, {`"2026-01-01T12:00:00.001Z"`, "upcoming"}, {`"2026-01-01T13:00:00+01:00"`, "aired"}, {`null`, "unknown"}, {`""`, "unknown"}, {`"invalid"`, "unknown"}, {`"2026-01-01"`, "unknown"}, {`{}`, "unknown"}, {`"0001-01-01T00:00:00Z"`, "unknown"}} {
		if got := episodeAirStatus(json.RawMessage(tc.raw), now); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
func TestTitleBrowseRejectsBadInputAndServiceFailures(t *testing.T) {
	c := &APIClient{}
	for _, o := range []TitleBrowseOptions{{Mode: "details", Type: "movie", ID: "../settings"}, {Mode: "details", Type: "person", ID: "55"}, {Mode: "details", Type: "movie", ID: "55", Season: browseInt(0)}, {Mode: "episodes", Type: "show", ID: "55"}, {Mode: "episodes", Type: "movie", ID: "55", Season: browseInt(1)}, {Mode: "episodes", Type: "show", ID: "55", Season: browseInt(-1)}, {Mode: "episode", Type: "show", ID: "55", Season: browseInt(1)}, {Mode: "episode", Type: "show", ID: "55", Season: browseInt(1), Episode: browseInt(10001)}} {
		if _, err := c.BrowseTitle(o); err == nil {
			t.Fatal("invalid request reached network")
		}
	}
	for _, status := range []int{401, 403, 404, 429, 500} {
		calls := 0
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != "GET" {
				t.Fatal("unexpected refresh/write")
			}
			w.WriteHeader(status)
			fmt.Fprint(w, "SYNTHETIC-PRIVATE")
		})
		c.Credentials.RefreshToken = "SYNTHETIC-REFRESH"
		_, err := c.BrowseTitle(TitleBrowseOptions{Mode: "details", Type: "movie", ID: "55"})
		if err == nil || calls != 1 || strings.Contains(err.Error(), "SYNTHETIC-PRIVATE") {
			t.Fatal("unsafe service failure", err, calls)
		}
	}
}
func TestTitleBrowseRefusesMalformedOrMismatchedResponses(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[{"season":1,"number":1}]`, `[{"season":2,"number":1,"ids":{"trakt":55}}]`, `[{"season":1,"number":1,"ids":{"trakt":55}},{"season":1,"number":1,"ids":{"trakt":56}}]`, `[{"season":1,"number":1,"ids":{"trakt":55}},{"season":1,"number":2,"ids":{"trakt":55}}]`} {
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := c.BrowseTitle(TitleBrowseOptions{Mode: "episodes", Type: "show", ID: "55", Season: browseInt(1)}); err == nil {
			t.Fatal("ambiguous response accepted", body)
		}
	}
	c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"title":"wrong media","ids":{"trakt":56}}`)
	})
	if _, err := c.BrowseTitle(TitleBrowseOptions{Mode: "details", Type: "show", ID: "55"}); err == nil {
		t.Fatal("wrong title accepted")
	}
}
