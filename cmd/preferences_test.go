package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPreferenceOfflinePreviews(t *testing.T) {
	for _, tc := range []struct {
		resource string
		args     []string
	}{
		{"ratings", []string{"set", "--type", "movie", "--ids", "55", "--rating", "8"}},
		{"ratings", []string{"remove", "--type", "episode", "--ids", "55"}},
		{"favorites", []string{"add", "--type", "show", "--ids", "55"}},
		{"favorites", []string{"remove", "--type", "movie", "--ids", "55"}},
	} {
		cmd := newPreferencesCmd(tc.resource)
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		var preview struct {
			Resource string `json:"resource"`
			Applied  bool   `json:"applied"`
			Path     string `json:"path"`
		}
		if err := json.Unmarshal(out.Bytes(), &preview); err != nil || preview.Applied || preview.Resource != tc.resource || preview.Path == "" {
			t.Fatalf("not a preview: %s %v", out, err)
		}
	}
}

func TestPreferenceInvalidCommandsFailBeforeCredentials(t *testing.T) {
	for _, tc := range []struct {
		resource string
		args     []string
	}{
		{"ratings", []string{"set", "--type", "movie", "--ids", "55", "--rating", "8", "--apply"}},
		{"ratings", []string{"set", "--type", "movie", "--ids", "55", "--rating", "8.5"}},
		{"ratings", []string{"set", "--type", "movie", "--ids", "55", "--rating", "11"}},
		{"ratings", []string{"set", "--type", "movie", "--ids", "55", "--rating", "8", "--rated-at", "yesterday"}},
		{"ratings", []string{"--page", "0"}},
		{"favorites", []string{"--type", "episode"}},
		{"favorites", []string{"add", "--type", "movie", "--ids", "55,55"}},
	} {
		cmd := newPreferencesCmd(tc.resource)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid command accepted", tc.args)
		}
	}
}
