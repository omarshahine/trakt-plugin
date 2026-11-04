package cmd

import (
	"bytes"
	"testing"
)

func TestTransferPreviewAndApplyGuard(t *testing.T) {
	for _, action := range []string{"copy", "move"} {
		c := newListTransferCmd(action)
		out := &bytes.Buffer{}
		c.SetOut(out)
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{"--source", "11", "--destination", "22", "--type", "movie", "--ids", "55"})
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"read_only":true`)) {
			t.Fatal("not preview")
		}
		c = newListTransferCmd(action)
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{"--source", "11", "--destination", "22", "--type", "movie", "--ids", "55", "--apply"})
		if err := c.Execute(); err == nil {
			t.Fatal("unguarded apply")
		}
	}
}
