package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const planAccount = `{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}}`

func planZIP(t *testing.T, history, watchlist, ratings string) string {
	t.Helper()
	return writeWebsiteComparisonZIP(t, map[string]string{
		"user-profile.json":      planAccount,
		"user-settings.json":     `{"browsing":{"watch_only_once":false}}`,
		"watched-history-1.json": history,
		"lists-watchlist-1.json": watchlist,
		"ratings-movies-1.json":  ratings,
	})
}

func TestRestorePlanMissingRecordsAndConflicts(t *testing.T) {
	source := planZIP(t, `[
		{"id":1,"watched_at":"2026-01-01T01:00:00+01:00","movie":{"ids":{"trakt":9007199254740993}}},
		{"id":2,"watched_at":"2026-02-01T00:00:00Z","movie":{"ids":{"trakt":9007199254740993}}}
	]`, `[{"id":3,"listed_at":"2026-01-01T00:00:00Z","type":"show","show":{"ids":{"trakt":55}}}]`,
		`[{"rating":8,"rated_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":11}}},{"rating":9,"movie":{"ids":{"trakt":12}}}]`)
	current := planZIP(t, `[{"id":99,"watched_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":9007199254740993}}}]`, `[]`,
		`[{"rating":5,"movie":{"ids":{"trakt":12}}}]`)
	p, err := PlanRestore(source, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Candidates) != 3 || len(p.Conflicts) != 1 {
		t.Fatalf("bad plan: %+v", p)
	}
	if p.Candidates[0].Item.WatchedAt != "2026-02-01T00:00:00Z" || p.Candidates[0].Item.TraktID != "9007199254740993" {
		t.Fatal("lost repeat watch or ID precision")
	}
	output := filepath.Join(t.TempDir(), "import.json")
	if err := WriteImportJSON(output, p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil || len(items) != 3 {
		t.Fatalf("bad array: %s %v", data, err)
	}
	for _, item := range items {
		for key := range item {
			if !strings.Contains(" trakt_id type watched_at watchlisted_at rating rated_at ", " "+key+" ") {
				t.Fatal("non-import field", key)
			}
		}
	}
	if err := WriteImportJSON(output, p); err == nil {
		t.Fatal("overwrote import")
	}
	self, err := PlanRestore(source, source)
	if err != nil || len(self.Candidates) != 0 || len(self.Conflicts) != 0 {
		t.Fatalf("self diff: %+v %v", self, err)
	}
}

func TestRestorePlanAmbiguousWatches(t *testing.T) {
	for _, history := range []string{
		`[{"id":1,"watched_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}},{"id":2,"watched_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}]`,
		`[{"id":1,"movie":{"ids":{"trakt":55}}}]`,
		`[{"id":1,"watched_at":null,"movie":{"ids":{"trakt":55}}}]`,
		`[{"id":1,"watched_at":"bad","movie":{"ids":{"trakt":55}}}]`,
		`[{"id":1,"watched_at":"unknown","show":{"ids":{"trakt":55}}}]`,
	} {
		p, err := PlanRestore(planZIP(t, history, `[]`, `[]`), planZIP(t, `[]`, `[]`, `[]`))
		if err != nil || len(p.Candidates) != 0 || len(p.Conflicts) == 0 {
			t.Fatalf("unsafe plan: %+v %v", p, err)
		}
	}
	source := planZIP(t, `[{"id":1,"watched_at":"unknown","movie":{"ids":{"trakt":55}}}]`, `[]`, `[]`)
	current := planZIP(t, `[{"id":9,"watched_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}]`, `[]`, `[]`)
	p, err := PlanRestore(source, current)
	if err != nil || len(p.Candidates) != 0 || len(p.Conflicts) != 1 {
		t.Fatalf("unknown duplicated: %+v %v", p, err)
	}
	p, err = PlanRestore(source, planZIP(t, `[]`, `[]`, `[]`))
	if err != nil || len(p.Candidates) != 1 || p.Candidates[0].Item.WatchedAt != "unknown" {
		t.Fatal("explicit unknown not preserved")
	}
}

func TestRestorePlanCoverageAccountAndIntegrity(t *testing.T) {
	source := planZIP(t, `[]`, `[{"id":1,"listed_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}]`, `[]`)
	current := writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": planAccount})
	p, err := PlanRestore(source, current)
	if err != nil || len(p.Candidates) != 0 || len(p.Unsupported) != 5 {
		t.Fatalf("coverage inferred: %+v %v", p, err)
	}
	for _, files := range []map[string]string{
		{"user-profile.json": `{"ids":{"slug":"other"}}`},
		{"watched-history-1.json": `[]`},
	} {
		if _, err := PlanRestore(source, writeWebsiteComparisonZIP(t, files)); err == nil {
			t.Fatal("unverified account accepted")
		}
	}
	f := fixture()
	f.changed = true
	o := options(t)
	if _, err := Export(f, o); err == nil {
		t.Fatal("fixture should fail")
	}
	if _, err := PlanRestore(source, o.Output); err == nil {
		t.Fatal("partial accepted")
	}
}

func TestRestorePlanListGapsNotWebsiteImport(t *testing.T) {
	files := map[string]string{"user-profile.json": planAccount, "lists-list-7-synthetic.json": `[{"id":1,"type":"movie","movie":{"ids":{"trakt":55}},"notes":"private note"}]`}
	source := writeWebsiteComparisonZIP(t, files)
	files["lists-list-7-synthetic.json"] = `[]`
	p, err := PlanRestore(source, writeWebsiteComparisonZIP(t, files))
	if err != nil || len(p.Candidates) != 0 || len(p.Unsupported) != 2 {
		t.Fatalf("bad list plan %+v %v", p, err)
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private note") {
		t.Fatal("leaked notes")
	}
}

func TestRestorePlanSameEventConflictAndInvalidRating(t *testing.T) {
	a := planZIP(t, `[{"id":1,"watched_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}]`, `[]`, `[{"rating":11,"movie":{"ids":{"trakt":55}}}]`)
	b := planZIP(t, `[{"id":1,"watched_at":"2026-01-02T00:00:00Z","movie":{"ids":{"trakt":55}}}]`, `[]`, `[]`)
	p, err := PlanRestore(a, b)
	if err != nil || len(p.Candidates) != 0 || len(p.Conflicts) != 2 {
		t.Fatalf("bad conflicts: %+v %v", p, err)
	}
}

func TestRestorePlanImportRejectsInvalidRowsBeforeCreatingFile(t *testing.T) {
	for _, item := range []ImportItem{
		{TraktID: "0", Type: "movie", Rating: 5},
		{TraktID: "55", Type: "person", Rating: 5},
		{TraktID: "55", Type: "movie", Rating: 11},
		{TraktID: "55", Type: "movie", WatchedAt: "yesterday"},
		{TraktID: "55", Type: "movie", WatchlistedAt: "unknown"},
		{TraktID: "55", Type: "show", WatchedAt: "unknown"},
		{TraktID: "55", Type: "movie", RatedAt: "2026-01-01T00:00:00Z"},
		{TraktID: "55", Type: "movie", Rating: 5, WatchedAt: "unknown"},
	} {
		p := RestorePlan{Version: 1, ReadOnly: true, AccountMatch: "matched", Candidates: []PlanCandidate{{Item: item}}}
		output := filepath.Join(t.TempDir(), "must-not-create.json")
		if err := WriteImportJSON(output, p); err == nil {
			t.Fatal("invalid import accepted")
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid row created a file")
		}
	}
}
