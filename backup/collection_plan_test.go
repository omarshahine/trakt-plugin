package backup

import (
	"encoding/json"
	"strings"
	"testing"
)

func collectionZIP(t *testing.T, movies, episodes, profile string) string {
	t.Helper()
	return writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": profile, "collection-movies-1.json": movies, "collection-episodes-1.json": episodes})
}
func TestCollectionPlanMissingConflictsAndNoDeletes(t *testing.T) {
	source := collectionZIP(t, `[{"movie":{"ids":{"trakt":55}},"collected_at":"2020-01-01T01:00:00+01:00","metadata":{"resolution":"hd_1080p","3d":false}},{"movie":{"ids":{"trakt":56}},"collected_at":"2021-01-01T00:00:00Z","metadata":null}]`, `[{"episode":{"ids":{"trakt":55}},"collected_at":"2020-01-01T00:00:00Z","metadata":{"audio":"dts"}}]`, planAccount)
	current := collectionZIP(t, `[{"movie":{"ids":{"trakt":55}},"collected_at":"2020-01-01T00:00:00.000Z","metadata":{"3d":false,"resolution":"hd_1080p"},"updated_at":"ignored"},{"movie":{"ids":{"trakt":57}},"collected_at":"2020-01-01T00:00:00Z"}]`, `[{"episode":{"ids":{"trakt":55}},"collected_at":"2022-01-01T00:00:00Z","metadata":{"audio":"aac","unknown":"PRIVATE CONFLICT VALUE"}}]`, planAccount)
	r, err := PlanCollectionRestore(source, current)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ReadOnly || len(r.Missing) != 1 || r.Missing[0].TraktID != "56" || r.Unchanged != 1 || r.CurrentOnly != 1 || len(r.Conflicts) != 1 || len(r.ReviewID) != 64 {
		t.Fatalf("bad plan %+v", r)
	}
	if strings.Join(r.Conflicts[0].Fields, ",") != "collected_at,metadata.audio,metadata.unknown" {
		t.Fatal(r.Conflicts)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "PRIVATE CONFLICT VALUE") {
		t.Fatal("conflict values leaked")
	}
	again, err := PlanCollectionRestore(source, current)
	if err != nil || again.ReviewID != r.ReviewID {
		t.Fatal("unstable digest", err)
	}
	reverse, err := PlanCollectionRestore(current, source)
	if err != nil || reverse.ReviewID == r.ReviewID || len(reverse.Missing) != 1 || reverse.Missing[0].TraktID != "57" {
		t.Fatal("direction not bound", err)
	}
}
func TestCollectionPlanRefusesUnsafeScopeAndBlocksInvalidSource(t *testing.T) {
	empty := collectionZIP(t, `[]`, `[]`, planAccount)
	invalidRows := []string{`[{"movie":{"ids":{"trakt":1}},"collected_at":"unknown"}]`, `[{"type":"show","movie":{"ids":{"trakt":1}},"collected_at":"2020-01-01T00:00:00Z"}]`, `[{"movie":{"ids":{"trakt":1}},"collected_at":"2020-01-01T00:00:00Z","metadata":[]}]`}
	for _, row := range invalidRows {
		bad := collectionZIP(t, row, `[]`, planAccount)
		r, err := PlanCollectionRestore(bad, empty)
		if err != nil || len(r.Blocked) != 1 || len(r.Missing) != 0 {
			t.Fatal("invalid source not withheld", err)
		}
		if _, err := PlanCollectionRestore(empty, bad); err == nil {
			t.Fatal("ambiguous current accepted")
		}
	}
	for _, bad := range []string{collectionZIP(t, `[]`, `[]`, `{"ids":{"slug":"other","uuid":"other"}}`), writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": planAccount, "collection-movies-1.json": "[]"})} {
		if _, err := PlanCollectionRestore(empty, bad); err == nil {
			t.Fatal("unsafe scope accepted")
		}
	}
}
func TestCollectionPlanDoesNotLoseLargeMetadataIntegers(t *testing.T) {
	a := collectionZIP(t, `[{"movie":{"ids":{"trakt":1}},"collected_at":"2020-01-01T00:00:00Z","metadata":{"unknown":9007199254740992}}]`, `[]`, planAccount)
	b := collectionZIP(t, `[{"movie":{"ids":{"trakt":1}},"collected_at":"2020-01-01T00:00:00Z","metadata":{"unknown":9007199254740993}}]`, `[]`, planAccount)
	r, err := PlanCollectionRestore(a, b)
	if err != nil || len(r.Conflicts) != 1 {
		t.Fatal("integer precision lost", err)
	}
}
