package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func ratingReviewZIP(t *testing.T, ratings, settings string) string {
	t.Helper()
	return writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": planAccount, "user-settings.json": settings, "ratings-movies-1.json": ratings})
}

func TestRatingRestoreReviewIsBoundedDeterministicAndAccountBound(t *testing.T) {
	settings := `{"user":{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}}}`
	source := ratingReviewZIP(t, `[{"rating":8,"rated_at":"2020-01-01T01:00:00+01:00","movie":{"ids":{"trakt":55}}},{"rating":9,"rated_at":"2021-01-01T00:00:00Z","movie":{"ids":{"trakt":56}}},{"rating":7,"movie":{"ids":{"trakt":57}}}]`, settings)
	current := ratingReviewZIP(t, `[]`, settings)
	r, err := PreviewRatingRestore(source, current, 1)
	if err != nil || len(r.Ratings) != 1 || r.Remaining != 1 || len(r.Blocked) != 1 || len(r.ReviewID) != 64 || r.Ratings[0].RatedAt != "2020-01-01T00:00:00Z" {
		t.Fatalf("bad review %+v %v", r, err)
	}
	again, err := PreviewRatingRestore(source, current, 1)
	if err != nil || again.ReviewID != r.ReviewID {
		t.Fatal("unstable review digest", err)
	}
	larger, err := PreviewRatingRestore(source, current, 2)
	if err != nil || len(larger.Ratings) != 2 || larger.Remaining != 0 || larger.ReviewID == r.ReviewID {
		t.Fatal("batch limit not bound", err)
	}
	copyPath := filepath.Join(t.TempDir(), "same-bytes-new-path.zip")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	copyReview, err := PreviewRatingRestore(copyPath, current, 1)
	if err != nil || copyReview.ReviewID != r.ReviewID {
		t.Fatal("digest depends on path instead of contents", err)
	}
	// A byte-only ZIP change must invalidate review, even if parsed rows match.
	if err := os.WriteFile(copyPath, append(data, []byte("synthetic zip comment suffix")...), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := PreviewRatingRestore(copyPath, current, 1)
	if err != nil || changed.ReviewID == r.ReviewID {
		t.Fatal("exact archive bytes not bound", err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := PreviewRatingRestore(source, current, limit); err == nil {
			t.Fatal("unsafe limit")
		}
	}
}

func TestRatingRestoreReviewExcludesConflictsAndOtherActions(t *testing.T) {
	source := writeWebsiteComparisonZIP(t, map[string]string{
		"user-profile.json":      planAccount,
		"ratings-movies-1.json":  `[{"rating":8,"rated_at":"2020-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}},{"rating":7,"movie":{"ids":{"trakt":56}}}]`,
		"watched-history-1.json": `[{"id":1,"watched_at":"2020-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}]`,
	})
	current := writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": planAccount, "ratings-movies-1.json": `[{"rating":3,"movie":{"ids":{"trakt":55}}}]`, "watched-history-1.json": `[]`})
	r, err := PreviewRatingRestore(source, current, 100)
	if err != nil || len(r.Ratings) != 0 || len(r.Blocked) != 2 {
		t.Fatalf("unsafe review %+v %v", r, err)
	}
	bad := writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": `{"ids":{"slug":"synthetic-user"}}`, "ratings-movies-1.json": `[]`})
	if _, err := PreviewRatingRestore(source, bad, 100); err == nil {
		t.Fatal("slug-only account adopted")
	}
	other := writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": `{"ids":{"slug":"other","uuid":"other-uuid"}}`, "ratings-movies-1.json": `[]`})
	if _, err := PreviewRatingRestore(source, other, 100); err == nil {
		t.Fatal("cross-account review accepted")
	}
}
