package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestListComparisonTypedDifferencesAndProposalLimits(t *testing.T) {
	state := func(entries []transferEntry) listEditState {
		s := listEditState{Items: map[string]json.RawMessage{}}
		for _, e := range entries {
			b, _ := json.Marshal(e)
			s.Items[e.ID.String()] = b
		}
		return s
	}
	a := state([]transferEntry{{ID: "1", Type: "movie", MediaID: "55", Rank: 1}, {ID: "2", Type: "show", MediaID: "55", Rank: 2}, {ID: "3", Type: "episode", MediaID: "66", Rank: 3}})
	b := state([]transferEntry{{ID: "4", Type: "movie", MediaID: "55", Rank: 1, Notes: json.RawMessage(`"note"`)}, {ID: "5", Type: "show", MediaID: "77", Rank: 2, Notes: json.RawMessage(`"keep"`)}, {ID: "6", Type: "movie", MediaID: "88", Rank: 3}, {ID: "7", Type: "person", MediaID: "99", Rank: 4}})
	r, err := compareListStates("sample", ListCompareOptions{Source: "10", Destination: "20", Limit: 1}, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ReadOnly || len(r.Shared) != 1 || len(r.SourceOnly) != 2 || len(r.DestinationOnly) != 3 || len(r.NoteDifferences) != 1 || len(r.Withheld) != 2 || r.CopyOmitted != 1 || len(r.Copies) != 1 || r.Copies[0].Type != "show" || len(r.Removals) != 1 || r.Removals[0].Applied {
		t.Fatal(r)
	}
	r, err = compareListStates("sample", ListCompareOptions{Source: "10", Destination: "10", Limit: 20}, a, a)
	if err != nil || len(r.Shared) != 3 || len(r.Copies) != 0 || len(r.Removals) != 0 {
		t.Fatal(r, err)
	}
}
func TestCompareValidatesBeforeNetwork(t *testing.T) {
	for _, o := range []ListCompareOptions{{Source: "../x", Destination: "20", Limit: 1}, {Source: "10", Destination: "20", Limit: 101}} {
		c := &APIClient{}
		if _, err := c.CompareLists(o); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}

func TestListCompareOnlyReadsAndRejectsChangingRevision(t *testing.T) {
	for _, changed := range []bool{false, true} {
		reads := 0
		c := mutationClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Fatal("unexpected write")
			}
			switch {
			case r.URL.Path == "/users/settings":
				fmt.Fprint(w, `{"user":{"ids":{"slug":"sample"}}}`)
			case strings.HasSuffix(r.URL.Path, "/items"):
				w.Header().Set("X-Pagination-Page", "1")
				w.Header().Set("X-Pagination-Limit", "1000")
				w.Header().Set("X-Pagination-Page-Count", "1")
				w.Header().Set("X-Pagination-Item-Count", "0")
				fmt.Fprint(w, `[]`)
			default:
				reads++
				version := "v1"
				if changed && reads >= 3 {
					version = "v2"
				}
				fmt.Fprintf(w, `{"ids":{"trakt":10},"user":{"ids":{"slug":"sample"}},"name":"Synthetic list","privacy":"private","updated_at":%q,"item_count":0}`, version)
			}
		})
		r, err := c.CompareLists(ListCompareOptions{Source: "10", Destination: "10", Limit: 1})
		if (err != nil) != changed {
			t.Fatal(r, err)
		}
		if !changed && !r.ReadOnly {
			t.Fatal(r)
		}
	}
}
