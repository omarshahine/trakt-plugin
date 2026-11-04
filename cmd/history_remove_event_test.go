package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
)

func TestHistoryRemoveEventOfflineCLI(t *testing.T) {
	args := []string{"--history-id", "9007199254740993", "--type", "episode", "--trakt-id", "55", "--watched-at", "2020-01-01T01:00:00.123+01:00"}
	command := newHistoryRemoveEventCmd()
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Target  api.HistoryEventRef `json:"target"`
		Applied bool                `json:"applied"`
		Note    string              `json:"note"`
	}
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Applied || p.Target.HistoryID != "9007199254740993" || p.Target.WatchedAt != "2020-01-01T00:00:00.123Z" || !strings.Contains(p.Note, "unverified") {
		t.Fatalf("invalid preview: %s", out)
	}
	command = newHistoryRemoveEventCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(append(args, "--apply"))
	if err := command.Execute(); err == nil {
		t.Fatal("missing expect-user accepted")
	}
}

func TestHistoryJSONCarriesPreciseRemovalGuards(t *testing.T) {
	date := time.Date(2020, 1, 1, 0, 0, 0, 123456789, time.UTC)
	movie := api.HistoryItem{ID: 9007199254740993, Type: "movie", WatchedAt: date}
	movie.Movie.Ids.Trakt = 55
	episode := api.HistoryItem{ID: 9007199254740994, Type: "episode", WatchedAt: date}
	episode.Episode.Ids.Trakt = 56
	episode.Show.Ids.Trakt = 999
	items := historyJSONItems(api.UserHistory{movie, episode})
	if items[0].HistoryID != "9007199254740993" || items[0].TraktID != "55" || items[1].TraktID != "56" || items[1].HistoryID != "9007199254740994" || items[0].WatchedAt != "2020-01-01T00:00:00.123456789Z" {
		t.Fatalf("lost precise event guards: %+v", items)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"history_id":"9007199254740993"`)) {
		t.Fatalf("event ID not a string: %s", encoded)
	}
}
