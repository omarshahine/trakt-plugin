package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestListUpdateOfflineAndRequiredAccount(t *testing.T) {
	c := newListUpdateCmd()
	out := &bytes.Buffer{}
	c.SetOut(out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--list", "55", "--description", ""})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Applied bool           `json:"applied"`
		Body    map[string]any `json:"body"`
	}
	if json.Unmarshal(out.Bytes(), &p) != nil || p.Applied || len(p.Body) != 1 || p.Body["description"] != "" {
		t.Fatal("bad clear preview", out)
	}
	for _, args := range [][]string{{"--list", "55"}, {"--list", "slug", "--name", "new"}, {"--list", "55", "--name", "new", "--apply"}} {
		c := newListUpdateCmd()
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatal("invalid edit accepted", args)
		}
	}
}
