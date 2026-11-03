package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func favoriteReviewZIP(t *testing.T, rows, profile string) string {
	t.Helper()
	return writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": profile, "lists-favorites-1.json": rows})
}
func TestFavoriteRestoreMembershipOnlyReviewAndDigest(t *testing.T) {
	source := favoriteReviewZIP(t, `[{"id":1,"type":"movie","movie":{"ids":{"trakt":55}},"rank":9,"notes":"PRIVATE NOTE NOT IN PLAN","listed_at":"old"},{"id":2,"type":"show","show":{"ids":{"trakt":55}}}]`, planAccount)
	current := favoriteReviewZIP(t, `[]`, planAccount)
	r, err := PreviewFavoriteRestore(source, current, 1)
	if err != nil || len(r.Favorites) != 1 || r.Remaining != 1 || len(r.ReviewID) != 64 || r.Kind != "favorites_membership" || len(r.OutOfScope) != 3 {
		t.Fatalf("bad review %+v %v", r, err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE NOTE") {
		t.Fatal("notes leaked into review")
	}
	again, err := PreviewFavoriteRestore(source, current, 1)
	if err != nil || again.ReviewID != r.ReviewID {
		t.Fatal("unstable review", err)
	}
	full, err := PreviewFavoriteRestore(source, current, 2)
	if err != nil || full.ReviewID == r.ReviewID || len(full.Favorites) != 2 {
		t.Fatal("batch options not bound", err)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.zip")
	bytes, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	copyReview, err := PreviewFavoriteRestore(copyPath, current, 1)
	if err != nil || copyReview.ReviewID != r.ReviewID {
		t.Fatal("path changed byte-bound review", err)
	}
	if err := os.WriteFile(copyPath, append(bytes, []byte("changed archive bytes")...), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := PreviewFavoriteRestore(copyPath, current, 1)
	if err != nil || changed.ReviewID == r.ReviewID {
		t.Fatal("changed bytes accepted under old review", err)
	}
	// Existing membership is left alone even when ranks/notes/dates differ.
	existing := favoriteReviewZIP(t, `[{"id":9,"type":"movie","movie":{"ids":{"trakt":55}},"rank":1,"notes":"CURRENT NOTE"}]`, planAccount)
	x, err := PreviewFavoriteRestore(source, existing, 100)
	if err != nil || len(x.Favorites) != 1 || x.Favorites[0].Type != "show" {
		t.Fatalf("metadata became a membership write: %+v %v", x, err)
	}
}
func TestFavoriteRestoreCoverageIdentityAndAmbiguity(t *testing.T) {
	source := favoriteReviewZIP(t, `[{"id":1,"type":"movie","movie":{"ids":{"trakt":55}}}]`, planAccount)
	for _, current := range []string{
		favoriteReviewZIP(t, `[]`, `{"ids":{"slug":"other","uuid":"other-uuid"}}`),
		favoriteReviewZIP(t, `[]`, `{"ids":{"slug":"synthetic-user"}}`),
		writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": planAccount}),
		favoriteReviewZIP(t, `[{"id":3,"type":"episode","episode":{"ids":{"trakt":55}}}]`, planAccount),
	} {
		if _, err := PreviewFavoriteRestore(source, current, 100); err == nil {
			t.Fatal("unsafe current scope accepted")
		}
	}
	current := favoriteReviewZIP(t, `[]`, planAccount)
	duplicates := favoriteReviewZIP(t, `[{"id":1,"type":"movie","movie":{"ids":{"trakt":55}}},{"id":2,"type":"movie","movie":{"ids":{"trakt":55}}}]`, planAccount)
	if _, err := PreviewFavoriteRestore(duplicates, current, 100); err == nil {
		t.Fatal("duplicate source membership accepted")
	}
	unsupported := favoriteReviewZIP(t, `[{"id":1,"type":"episode","episode":{"ids":{"trakt":55}}}]`, planAccount)
	r, err := PreviewFavoriteRestore(unsupported, current, 100)
	if err != nil || len(r.Favorites) != 0 || len(r.Blocked) != 1 {
		t.Fatalf("unsupported membership not withheld: %+v %v", r, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := PreviewFavoriteRestore(source, current, limit); err == nil {
			t.Fatal("unsafe limit")
		}
	}
}
