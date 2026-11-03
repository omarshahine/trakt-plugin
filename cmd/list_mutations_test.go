package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestListManagementDefaultsToOfflinePreview(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--name", "Synthetic"},
		{"add", "--list", "7", "--type", "movie", "--ids", "55,56"},
		{"remove", "--list", "7", "--type", "episode", "--ids", "55"},
		{"notes", "--list", "7", "--item", "900", "--notes="},
		{"reorder", "--list", "7", "--rank", "901,900"},
	} {
		cmd := newListsCmd()
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var preview struct {
			Applied bool   `json:"applied"`
			Action  string `json:"action"`
			Method  string `json:"method"`
		}
		if err := json.Unmarshal(out.Bytes(), &preview); err != nil || preview.Applied || preview.Action != args[0] || preview.Method == "" {
			t.Fatalf("not a preview: %s %v", out, err)
		}
	}
}

func TestListManagementRejectsInvalidBeforeAuth(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--name", ""},
		{"add", "--list", "7", "--type", "movie", "--ids", "55,55", "--apply"},
		{"notes", "--list", "7", "--item", "900", "--apply"},
		{"reorder", "--list", "7", "--rank", "55,55", "--apply"},
	} {
		cmd := newListsCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
}
