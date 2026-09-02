package api

import (
	"encoding/json"
	"testing"
)

// Trakt answers POST /sync/history with 201 even when it rejects individual
// season/episode selectors; those land in not_found.seasons/episodes and must
// survive decoding so partial writes can be reported as partial.
func TestSyncHistoryRespDecodeNotFoundSelectors(t *testing.T) {
	body := `{
		"added": {"movies": 0, "episodes": 2},
		"not_found": {
			"movies": [],
			"shows": [],
			"seasons": [{"number": 99}],
			"episodes": [{"season": 2, "number": 50}]
		}
	}`

	var resp SyncHistoryResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Added.Episodes != 2 {
		t.Errorf("added.episodes = %d, want 2", resp.Added.Episodes)
	}
	if len(resp.NotFound.Seasons) != 1 {
		t.Errorf("not_found.seasons = %d entries, want 1", len(resp.NotFound.Seasons))
	}
	if len(resp.NotFound.Episodes) != 1 {
		t.Errorf("not_found.episodes = %d entries, want 1", len(resp.NotFound.Episodes))
	}
	if len(resp.NotFound.Movies) != 0 || len(resp.NotFound.Shows) != 0 {
		t.Errorf("not_found movies/shows should decode empty, got %d/%d",
			len(resp.NotFound.Movies), len(resp.NotFound.Shows))
	}
}
