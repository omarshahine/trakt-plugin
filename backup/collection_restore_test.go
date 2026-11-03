package backup

import (
	"testing"
)

func TestCollectionRestoreReviewSeparatesObservationsAndExecutableScope(t *testing.T) {
	source := collectionZIP(t, `[{"movie":{"ids":{"trakt":55}},"collected_at":"2020-01-01T00:00:00Z","metadata":{"3d":null,"hdr":null,"audio":"dts"}},{"movie":{"ids":{"trakt":56}},"collected_at":"2020-01-01T00:00:00Z","metadata":{"3d":1}}]`, `[{"episode":{"ids":{"trakt":55}},"collected_at":"2020-01-01T00:00:00Z"}]`, planAccount)
	current := collectionZIP(t, `[]`, `[]`, planAccount)
	r, err := PreviewCollectionRestore(source, current, 1)
	if err != nil || len(r.Collections) != 1 || len(r.Blocked) != 1 || r.Remaining != 1 || len(r.ReviewID) != 64 {
		t.Fatal(r, err)
	}
	observation, err := PlanCollectionRestore(source, current)
	if err != nil || observation.ReviewID == r.ReviewID {
		t.Fatal("observation digest authorized execution", err)
	}
	full, err := PreviewCollectionRestore(source, current, 100)
	if err != nil || len(full.Collections) != 2 || full.ReviewID == r.ReviewID {
		t.Fatal(full, err)
	}
	same, err := PreviewCollectionRestore(source, current, 1)
	if err != nil || same.ReviewID != r.ReviewID {
		t.Fatal("unstable review", err)
	}
	if _, err := PreviewCollectionRestore(current, source, 100); err == nil {
		t.Fatal("unsupported current metadata accepted")
	}
	for _, limit := range []int{0, 101} {
		if _, err := PreviewCollectionRestore(source, current, limit); err == nil {
			t.Fatal("unbounded batch")
		}
	}
	bad := collectionZIP(t, `[]`, `[]`, `{"ids":{"slug":"other","uuid":"other"}}`)
	if _, err := PreviewCollectionRestore(source, bad, 100); err == nil {
		t.Fatal("wrong UUID")
	}
}
