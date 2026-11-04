package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func cmdRatingsZIP(t *testing.T, ratings string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ratings.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range map[string]string{"user-profile.json": `{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}}`, "ratings-movies-1.json": ratings} {
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
	return path
}
func TestRatingRestoreCLIRequiresReviewedDigestBeforeAuth(t *testing.T) {
	source := cmdRatingsZIP(t, `[{"rating":8,"rated_at":"2020-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}]`)
	current := cmdRatingsZIP(t, `[]`)
	args := []string{"--source", source, "--current", current}
	command := newBackupRestoreRatingsCmd()
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var preview struct {
		ReviewID string `json:"review_id"`
		ReadOnly bool   `json:"read_only"`
	}
	if err := json.Unmarshal(out.Bytes(), &preview); err != nil || !preview.ReadOnly || len(preview.ReviewID) != 64 {
		t.Fatalf("invalid review %s %v", out, err)
	}
	for _, extra := range [][]string{{"--apply"}, {"--apply", "--review-id", "wrong", "--expect-user", "synthetic-user"}, {"--apply", "--review-id", preview.ReviewID, "--expect-user", "other"}} {
		c := newBackupRestoreRatingsCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(append(append([]string{}, args...), extra...))
		if err := c.Execute(); err == nil {
			t.Fatal("unreviewed or mismatched apply accepted")
		}
	}
}
