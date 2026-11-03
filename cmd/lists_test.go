package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/omarshahine/trakt-plugin/api"
)

func TestListsRejectInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"items"}, {"items", "--list="}, {"--page", "0"}, {"--limit", "1001"}, {"--limit", "-1"}, {"unexpected"}} {
		cmd := newListsCmd()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestListsJSONPreservesMetadataAndEmptyArray(t *testing.T) {
	for _, records := range [][]json.RawMessage{nil, {json.RawMessage(`{"id":12,"rank":3,"notes":"synthetic","type":"person","person":{"ids":{"trakt":456}}}`)}} {
		var buf bytes.Buffer
		if err := writeCustomLists(&buf, records, api.Pagination{Page: "2", PageCount: "3", ItemCount: "21", Limit: "10"}, true); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Items []json.RawMessage `json:"items"`
			api.Pagination
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Items == nil || len(got.Items) != len(records) || got.Page != "2" || got.ItemCount != "21" {
			t.Fatalf("output: %s", buf.String())
		}
		if len(records) > 0 && !strings.Contains(buf.String(), `"notes": "synthetic"`) {
			t.Fatal("lost notes")
		}
	}
}
