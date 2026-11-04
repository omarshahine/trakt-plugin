package api

import (
	"bytes"
	"fmt"
	"sort"
)

type ListCompareOptions struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Limit       int    `json:"limit"`
}
type ListCompareResult struct {
	ReadOnly            bool                  `json:"read_only"`
	Account             string                `json:"account"`
	Options             ListCompareOptions    `json:"options"`
	SourceRevision      string                `json:"source_revision"`
	DestinationRevision string                `json:"destination_revision"`
	SourceHash          string                `json:"source_hash"`
	DestinationHash     string                `json:"destination_hash"`
	Shared              []transferEntry       `json:"shared"`
	SourceOnly          []transferEntry       `json:"source_only"`
	DestinationOnly     []transferEntry       `json:"destination_only"`
	NoteDifferences     []string              `json:"note_differences"`
	Copies              []ListTransferPreview `json:"copy_previews"`
	Removals            []ListChangePreview   `json:"removal_previews"`
	Withheld            []string              `json:"withheld"`
	CopyOmitted         int                   `json:"copy_omitted"`
	RemovalOmitted      int                   `json:"removal_omitted"`
}

func ValidateListCompare(o ListCompareOptions) error {
	for _, id := range []string{o.Source, o.Destination} {
		if _, err := listID(id); err != nil {
			return err
		}
	}
	if o.Limit < 1 || o.Limit > 100 {
		return fmt.Errorf("limit must be 1-100 per proposal direction")
	}
	return nil
}
func (c *APIClient) CompareLists(o ListCompareOptions) (ListCompareResult, error) {
	if err := ValidateListCompare(o); err != nil {
		return ListCompareResult{}, err
	}
	reader := *c
	reader.Credentials.RefreshToken = ""
	c = &reader
	owner, err := c.ResolveListOwner()
	if err != nil {
		return ListCompareResult{}, err
	}
	a, err := c.stableListEditState(owner, o.Source)
	if err != nil {
		return ListCompareResult{}, err
	}
	b := a
	if o.Source != o.Destination {
		b, err = c.stableListEditState(owner, o.Destination)
		if err != nil {
			return ListCompareResult{}, err
		}
	}
	result, err := compareListStates(owner, o, a, b)
	if err != nil {
		return ListCompareResult{}, err
	}
	for _, x := range []struct {
		id    string
		state listEditState
	}{{o.Source, a}, {o.Destination, b}} {
		now, err := c.stableListEditState(owner, x.id)
		if err != nil {
			return ListCompareResult{}, err
		}
		if now.ItemsHash != x.state.ItemsHash || now.Updated != x.state.Updated || !sameEditSettings(now, x.state, nil) {
			return ListCompareResult{}, fmt.Errorf("list changed during comparison; proposals withheld")
		}
	}
	return result, nil
}
func compareListStates(owner string, o ListCompareOptions, a, b listEditState) (ListCompareResult, error) {
	r := ListCompareResult{ReadOnly: true, Account: owner, Options: o, SourceRevision: a.Updated, DestinationRevision: b.Updated, SourceHash: a.ItemsHash, DestinationHash: b.ItemsHash, Shared: []transferEntry{}, SourceOnly: []transferEntry{}, DestinationOnly: []transferEntry{}, NoteDifferences: []string{}, Copies: []ListTransferPreview{}, Removals: []ListChangePreview{}, Withheld: []string{}}
	if len(a.Items) > 1000 || len(b.Items) > 1000 {
		return r, fmt.Errorf("comparison report supports at most 1000 entries per list")
	}
	left, err := transferIndex(a)
	if err != nil {
		return r, err
	}
	right, err := transferIndex(b)
	if err != nil {
		return r, err
	}
	for key, x := range left {
		if y, ok := right[key]; ok {
			r.Shared = append(r.Shared, x)
			if !bytes.Equal(bytes.TrimSpace(x.Notes), bytes.TrimSpace(y.Notes)) {
				r.NoteDifferences = append(r.NoteDifferences, key)
			}
		} else {
			r.SourceOnly = append(r.SourceOnly, x)
		}
	}
	for key, x := range right {
		if _, ok := left[key]; !ok {
			r.DestinationOnly = append(r.DestinationOnly, x)
		}
	}
	for _, rows := range [][]transferEntry{r.Shared, r.SourceOnly, r.DestinationOnly} {
		sort.Slice(rows, func(i, j int) bool { return rows[i].Rank < rows[j].Rank })
	}
	sort.Strings(r.NoteDifferences)
	groups := func(rows []transferEntry, removal bool) (map[string][]string, int) {
		out := map[string][]string{}
		n, omitted := 0, 0
		for _, x := range rows {
			key := x.Type + ":" + x.MediaID
			if x.Type == "person" {
				r.Withheld = append(r.Withheld, key+": unsupported proposal type")
				continue
			}
			if removal && len(x.Notes) > 0 && string(x.Notes) != "null" && string(x.Notes) != `""` {
				r.Withheld = append(r.Withheld, key+": removal would discard notes")
				continue
			}
			if n == o.Limit {
				omitted++
				continue
			}
			out[x.Type] = append(out[x.Type], x.MediaID)
			n++
		}
		return out, omitted
	}
	copyGroups, n := groups(r.SourceOnly, false)
	r.CopyOmitted = n
	removeGroups, n := groups(r.DestinationOnly, true)
	r.RemovalOmitted = n
	for _, kind := range []string{"movie", "show", "season", "episode"} {
		if ids := copyGroups[kind]; len(ids) > 0 {
			p, err := PreviewListTransfer(ListTransfer{Action: "copy", Source: o.Source, Destination: o.Destination, Type: kind, IDs: ids})
			if err != nil {
				return r, err
			}
			r.Copies = append(r.Copies, p)
		}
		if ids := removeGroups[kind]; len(ids) > 0 {
			p, err := PreviewListChange(owner, ListChange{Action: "remove", List: o.Destination, Type: kind, IDs: ids})
			if err != nil {
				return r, err
			}
			r.Removals = append(r.Removals, p)
		}
	}
	return r, nil
}
