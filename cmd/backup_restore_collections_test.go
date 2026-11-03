package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func cmdCollectionsZIP(t *testing.T, rows string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "collections.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range map[string]string{"user-profile.json": `{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}}`, "collection-movies-1.json": rows, "collection-episodes-1.json": "[]"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestCollectionRestoreCLIIsOfflineAndRequiresReview(t *testing.T) {
	source := cmdCollectionsZIP(t, `[{"movie":{"ids":{"trakt":55}},"collected_at":"2020-01-01T00:00:00Z"}]`)
	current := cmdCollectionsZIP(t, `[]`)
	args := []string{"--source", source, "--current", current}
	c := newBackupRestoreCollectionsCmd()
	out := &bytes.Buffer{}
	c.SetOut(out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var r struct {
		ReviewID string `json:"review_id"`
		ReadOnly bool   `json:"read_only"`
		Kind     string `json:"kind"`
	}
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || !r.ReadOnly || len(r.ReviewID) != 64 || r.Kind != "collection_missing_v1" {
		t.Fatalf("bad preview %s %v", out, err)
	}
	for _, extra := range [][]string{{"--apply"}, {"--apply", "--review-id", "wrong", "--expect-user", "synthetic-user"}, {"--apply", "--review-id", r.ReviewID, "--expect-user", "other"}} {
		cmd := newBackupRestoreCollectionsCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(append(append([]string{}, args...), extra...))
		if err := cmd.Execute(); err == nil {
			t.Fatal("unreviewed apply accepted")
		}
	}
}
