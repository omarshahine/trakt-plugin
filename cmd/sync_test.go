package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
)

func TestBuildSyncReq(t *testing.T) {
	req := buildSyncReq([]syncPlanned{
		{Kind: "episode", TraktID: 10, Season: 2, Episode: 1, WatchedAt: "2026-01-02T03:04:05Z"},
		{Kind: "episode", TraktID: 10, Season: 1, Episode: 5, WatchedAt: "2026-01-01T00:00:00Z"},
		{Kind: "episode", TraktID: 20, Season: 0, Episode: 1, WatchedAt: "2026-01-03T00:00:00Z"},
		{Kind: "movie", TraktID: 30, WatchedAt: "2026-01-04T00:00:00Z"},
	})
	if len(req.Movies) != 1 || req.Movies[0].Ids.Trakt != 30 || req.Movies[0].WatchedAt == "" {
		t.Fatalf("movies = %+v", req.Movies)
	}
	if len(req.Shows) != 2 || req.Shows[0].Ids.Trakt != 10 {
		t.Fatalf("shows = %+v", req.Shows)
	}
	s := req.Shows[0].Seasons
	if len(s) != 2 || s[0].Number != 1 || s[1].Number != 2 || s[1].Episodes[0].WatchedAt != "2026-01-02T03:04:05Z" {
		t.Errorf("seasons = %+v", s)
	}
	b, _ := json.Marshal(req)
	if !strings.Contains(string(b), `"number":0`) {
		t.Errorf("season 0 must serialize: %s", b)
	}
}

func TestHistoryJSONKeepsSeasonZero(t *testing.T) {
	var h api.HistoryItem
	h.Type = "episode"
	h.Episode.Season, h.Episode.Number = 0, 3
	h.Show.Title = "Example"
	h.WatchedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var m api.HistoryItem
	m.Type = "movie"
	m.Movie.Title = "Film"
	m.WatchedAt = h.WatchedAt
	b, _ := json.Marshal(historyJSONItems(api.UserHistory{h, m}))
	out := string(b)
	if !strings.Contains(out, `"season":0`) || !strings.Contains(out, `"episode":3`) {
		t.Errorf("season 0 dropped: %s", out)
	}
	if strings.Count(out, `"season"`) != 1 {
		t.Errorf("movies must not carry season: %s", out)
	}
}

func TestSyncOffWithoutConfig(t *testing.T) {
	t.Setenv("TRAKT_SYNC_CONFIG", t.TempDir()+"/absent.yaml")
	jsonOutput = true
	defer func() { jsonOutput = false }()
	cmd := syncCmd
	_ = cmd.Flags().Set("config", "")
	if err := runSync(cmd, nil); err != nil {
		t.Fatalf("sync without config must exit cleanly, got %v", err)
	}
}

func TestUpdateSyncListsKeepsComments(t *testing.T) {
	in := []byte("enabled: true\n# who\ndeny:\n  - Kids Show # theirs\n  - Other Show\n")
	out, changed, err := updateSyncLists(in, []string{"kids show", "New Drama"}, "allow", "deny")
	if err != nil || !changed {
		t.Fatalf("err=%v changed=%v", err, changed)
	}
	s := string(out)
	for _, want := range []string{"# who", "Other Show", "allow:", "- kids show", "- New Drama"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Kids Show") {
		t.Errorf("approved title should leave deny:\n%s", s)
	}
	if _, changed, _ := updateSyncLists(out, []string{"New Drama"}, "allow", "deny"); changed {
		t.Error("re-approving should be a no-op")
	}
}
