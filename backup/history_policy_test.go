package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreRepeatWatchPolicy(t *testing.T) {
	first := `{"id":1,"watched_at":"2020-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}`
	second := `{"id":2,"watched_at":"2021-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}`
	for _, tc := range []struct {
		name, settings, policy string
		allowed                bool
	}{
		{"allowed", `{"browsing":{"watch_only_once":false}}`, "multiple_plays_allowed", true},
		{"disabled", `{"browsing":{"watch_only_once":true}}`, "watch_only_once", false},
		{"missing", `{"synthetic":true}`, "unknown", false},
		{"null", `{"browsing":{"watch_only_once":null}}`, "unknown", false},
		{"string_false", `{"browsing":{"watch_only_once":"false"}}`, "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, currentHistory := range []string{"[]", "[" + first + "]"} {
				current := writeWebsiteComparisonZIP(t, map[string]string{
					"user-profile.json": planAccount, "user-settings.json": tc.settings,
					"watched-history-1.json": currentHistory, "lists-watchlist-1.json": `[]`, "ratings-movies-1.json": `[]`,
				})
				source := planZIP(t, "["+first+","+second+"]", `[]`, `[]`)
				p, err := PlanRestore(source, current)
				if err != nil {
					t.Fatal(err)
				}
				if p.HistoryPolicy != tc.policy {
					t.Fatalf("policy %s", p.HistoryPolicy)
				}
				want := 2
				if currentHistory != "[]" {
					want = 1
				}
				if !tc.allowed {
					if len(p.Candidates) != 0 || len(p.Conflicts) != want {
						t.Fatalf("unsafe repeat plan: %+v", p)
					}
				} else if len(p.Candidates) != want || len(p.Conflicts) != 0 {
					t.Fatalf("valid repeats lost: %+v", p)
				}
				out := filepath.Join(t.TempDir(), "review.json")
				if err := WriteImportJSON(out, p); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				var items []ImportItem
				if err := json.Unmarshal(data, &items); err != nil {
					t.Fatal(err)
				}
				if !tc.allowed && len(items) != 0 {
					t.Fatalf("unsafe watches exported: %s", data)
				}
				// No-change plans remain no-ops, even when policy is unknown.
				self, err := PlanRestore(current, current)
				if err != nil || len(self.Candidates) != 0 || len(self.Conflicts) != 0 {
					t.Fatalf("self plan: %+v %v", self, err)
				}
			}
		})
	}
}

func TestRestoreWatchPolicyDoesNotBlockUnrelatedAdditions(t *testing.T) {
	source := planZIP(t, `[{"id":1,"watched_at":"2020-01-01T00:00:00Z","episode":{"ids":{"trakt":55}}}]`, `[]`, `[{"rating":8,"movie":{"ids":{"trakt":66}}}]`)
	current := writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": planAccount, "user-settings.json": `{"browsing":{"watch_only_once":true}}`, "watched-history-1.json": `[]`, "ratings-movies-1.json": `[]`, "lists-watchlist-1.json": `[]`})
	p, err := PlanRestore(source, current)
	if err != nil || len(p.Candidates) != 2 || len(p.Conflicts) != 0 {
		t.Fatalf("unrelated additions blocked: %+v %v", p, err)
	}
}

func TestArchiveWatchPolicyUsesRawAPINotHistoricalSettings(t *testing.T) {
	a := &comparisonArchive{data: map[string]map[string]json.RawMessage{
		"user/settings":         {"snapshot": json.RawMessage(`{"browsing":{"watch_only_once":false}}`)},
		"raw-api/user/settings": {"snapshot": json.RawMessage(`{"browsing":{"watch_only_once":true}}`)},
	}}
	got := archiveWatchOnlyOnce(a)
	if got == nil || !*got {
		t.Fatal("ignored raw API preference")
	}
	a.data["raw-api/user/settings"]["snapshot"] = json.RawMessage(`{}`)
	if archiveWatchOnlyOnce(a) != nil {
		t.Fatal("fell back to historical preference")
	}
}

func TestRestoreEpisodeRepeatIsWithheld(t *testing.T) {
	source := planZIP(t, `[{"id":1,"watched_at":"2020-01-01T00:00:00Z","episode":{"ids":{"trakt":55}}}]`, `[]`, `[]`)
	current := writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": planAccount, "watched-history-1.json": `[{"id":2,"watched_at":"2021-01-01T00:00:00Z","episode":{"ids":{"trakt":55}}}]`})
	p, err := PlanRestore(source, current)
	if err != nil || len(p.Candidates) != 0 || len(p.Conflicts) != 1 || !strings.Contains(p.Conflicts[0].Reason, "repeat watch") {
		t.Fatalf("unsafe episode repeat: %+v %v", p, err)
	}
}

func TestRestoreNativeWatchPolicySurvivesArchiveValidation(t *testing.T) {
	f := fixture()
	f.documentData = map[string]json.RawMessage{
		"user/settings": raw(`{"user":{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}},"browsing":{"watch_only_once":false}}`),
		"user/profile":  raw(planAccount), "user/stats": raw(`{"synthetic":true}`),
	}
	f.data["history"] = []json.RawMessage{
		raw(`{"id":1,"watched_at":"2020-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}`),
		raw(`{"id":2,"watched_at":"2021-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}`),
	}
	source := nativeComparisonFixture(t, f)
	f.documentData["user/settings"] = raw(`{"user":{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}},"browsing":{"watch_only_once":true}}`)
	f.data["history"] = f.data["history"][:1]
	current := nativeComparisonFixture(t, f)
	p, err := PlanRestore(source, current)
	if err != nil {
		t.Fatal(err)
	}
	if p.HistoryPolicy != "watch_only_once" {
		t.Fatalf("source preference used instead of current: %+v", p)
	}
	for _, candidate := range p.Candidates {
		if candidate.Resource == "history" {
			t.Fatal("unsafe native repeat exported")
		}
	}
	found := false
	for _, issue := range p.Conflicts {
		if issue.Resource == "history" && strings.Contains(issue.Reason, "repeat watch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing native repeat conflict: %+v", p)
	}
}
