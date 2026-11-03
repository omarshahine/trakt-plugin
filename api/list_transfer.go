package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ListTransfer struct {
	Action      string   `json:"action"`
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	IDs         []string `json:"ids"`
}
type ListTransferPreview struct {
	ListTransfer
	Applied    bool     `json:"applied"`
	ReadOnly   bool     `json:"read_only"`
	OutOfScope []string `json:"out_of_scope"`
	Note       string   `json:"note"`
}
type ListTransferResult struct {
	Action              string            `json:"action"`
	Outcome             string            `json:"outcome"`
	Stage               string            `json:"stage"`
	Attempted           bool              `json:"attempted"`
	AddAttempted        bool              `json:"add_attempted"`
	RemoveAttempted     bool              `json:"remove_attempted"`
	DestinationVerified bool              `json:"destination_verified"`
	Verified            bool              `json:"verified"`
	Account             string            `json:"account,omitempty"`
	Source              string            `json:"source"`
	Destination         string            `json:"destination"`
	Add                 *ListChangeResult `json:"add,omitempty"`
	Remove              *ListChangeResult `json:"remove,omitempty"`
	Type                string            `json:"type"`
	IDs                 []string          `json:"ids"`
}

func PreviewListTransfer(x ListTransfer) (ListTransferPreview, error) {
	p := ListTransferPreview{ListTransfer: x, ReadOnly: true, Note: "Offline membership-only preview. Move refuses source notes; new dates/ranks are server-assigned. Apply requires expect-user and fresh ownership/readback checks.", OutOfScope: []string{"notes_copy", "listed_at_preservation", "new_item_rank", "list_settings"}}
	if x.Action != "copy" && x.Action != "move" {
		return p, fmt.Errorf("action must be copy or move")
	}
	for _, id := range []string{x.Source, x.Destination} {
		if _, err := listID(id); err != nil {
			return p, fmt.Errorf("source/destination must be canonical numeric LIST IDs")
		}
	}
	if x.Source == x.Destination {
		return p, fmt.Errorf("source and destination must differ")
	}
	if len(x.IDs) < 1 || len(x.IDs) > 100 {
		return p, fmt.Errorf("transfer 1-100 media IDs per batch")
	}
	if _, err := PreviewListChange("me", ListChange{Action: "add", List: x.Destination, Type: x.Type, IDs: x.IDs}); err != nil {
		return p, err
	}
	return p, nil
}

type transferEntry struct {
	ID      json.Number     `json:"id"`
	Type    string          `json:"type"`
	MediaID string          `json:"media_id"`
	Rank    int             `json:"rank"`
	Listed  json.RawMessage `json:"listed_at"`
	Notes   json.RawMessage `json:"notes"`
}

func transferIndex(state listEditState) (map[string]transferEntry, error) {
	result := map[string]transferEntry{}
	for _, raw := range state.Items {
		var e transferEntry
		if json.Unmarshal(raw, &e) != nil {
			return nil, fmt.Errorf("invalid transfer item")
		}
		key := e.Type + ":" + e.MediaID
		if _, ok := result[key]; ok {
			return nil, fmt.Errorf("duplicate typed list membership")
		}
		result[key] = e
	}
	return result, nil
}

// Preserves original identities/data and relative rank order, allowing only
// explicitly removed source keys or newly added destination keys. New item
// ranks/dates are server-assigned, not fabricated or copied from the source.
func transferPreserved(before, after listEditState, removed, added map[string]bool) bool {
	a, err := transferIndex(before)
	if err != nil {
		return false
	}
	b, err := transferIndex(after)
	if err != nil {
		return false
	}
	expected := len(a)
	for key := range removed {
		if _, ok := a[key]; ok {
			expected--
		}
	}
	for key := range added {
		if _, ok := a[key]; !ok {
			expected++
		}
	}
	if len(b) != expected || !sameEditSettings(before, after, nil) {
		return false
	}
	oldOrder, newOrder := []transferEntry{}, []transferEntry{}
	for key, old := range a {
		if removed[key] {
			if _, ok := b[key]; ok {
				return false
			}
			continue
		}
		now, ok := b[key]
		if !ok {
			return false
		}
		oldRank, newRank := old.Rank, now.Rank
		old.Rank = 0
		now.Rank = 0
		oldJSON, _ := json.Marshal(old)
		nowJSON, _ := json.Marshal(now)
		if string(oldJSON) != string(nowJSON) {
			return false
		}
		old.Rank = oldRank
		now.Rank = newRank
		oldOrder = append(oldOrder, old)
		newOrder = append(newOrder, now)
	}
	for key := range b {
		if _, ok := a[key]; !ok && !added[key] {
			return false
		}
	}
	for key := range added {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	sort.Slice(oldOrder, func(i, j int) bool { return oldOrder[i].Rank < oldOrder[j].Rank })
	sort.Slice(newOrder, func(i, j int) bool { return newOrder[i].Rank < newOrder[j].Rank })
	for i := range oldOrder {
		if oldOrder[i].ID != newOrder[i].ID {
			return false
		}
	}
	return true
}
func (c *APIClient) ApplyListTransfer(expected string, x ListTransfer) (ListTransferResult, error) {
	r := ListTransferResult{Action: x.Action, Outcome: "not_attempted", Stage: "preflight", Source: x.Source, Destination: x.Destination, Type: x.Type, IDs: x.IDs}
	if _, err := PreviewListTransfer(x); err != nil {
		return r, err
	}
	if strings.TrimSpace(expected) == "" {
		return r, fmt.Errorf("expect-user required")
	}
	reader := *c
	reader.Credentials.RefreshToken = ""
	c = &reader
	owner, err := c.ResolveListOwner()
	if err != nil {
		return r, err
	}
	if owner != expected {
		return r, fmt.Errorf("live account does not match expect-user")
	}
	r.Account = owner
	source, err := c.stableListEditState(owner, x.Source)
	if err != nil {
		return r, err
	}
	dest, err := c.stableListEditState(owner, x.Destination)
	if err != nil {
		return r, err
	}
	src, err := transferIndex(source)
	if err != nil {
		return r, err
	}
	dst, err := transferIndex(dest)
	if err != nil {
		return r, err
	}
	addIDs, removeIDs := []string{}, []string{}
	added, removed := map[string]bool{}, map[string]bool{}
	for _, id := range x.IDs {
		key := x.Type + ":" + id
		s, sOK := src[key]
		_, dOK := dst[key]
		if !sOK && !(x.Action == "move" && dOK) {
			return r, fmt.Errorf("requested media is missing from source (and is not an already-at-destination move); no writes")
		}
		if x.Action == "move" && sOK {
			if len(s.Notes) > 0 && string(s.Notes) != "null" {
				var note string
				if json.Unmarshal(s.Notes, &note) != nil || note != "" {
					return r, fmt.Errorf("source item has notes; membership-only move refuses metadata loss")
				}
			}
			removeIDs = append(removeIDs, id)
			removed[key] = true
		}
		if !dOK {
			addIDs = append(addIDs, id)
			added[key] = true
		}
	}
	if len(addIDs) > 0 {
		r.Stage = "add"
		r.Attempted = true
		r.AddAttempted = true
		result, err := c.ApplyListChange(owner, ListChange{Action: "add", List: x.Destination, Type: x.Type, IDs: addIDs})
		r.Add = &result
		if err != nil {
			r.Outcome = "partial"
			if result.Outcome == "unknown" {
				r.Outcome = "unknown"
			}
			return r, fmt.Errorf("destination add not confirmed; SOURCE REMOVAL NOT ATTEMPTED; inspect before explicit follow-up: %w", err)
		}
	}
	r.Stage = "destination_readback"
	destAfter, err := c.stableListEditState(owner, x.Destination)
	if err != nil {
		r.Outcome = "unknown"
		return r, fmt.Errorf("destination readback failed; source removal not attempted: %w", err)
	}
	if !transferPreserved(dest, destAfter, nil, added) {
		r.Outcome = "partial"
		return r, fmt.Errorf("destination not fully verified; source removal not attempted")
	}
	r.DestinationVerified = true
	r.Stage = "source_recheck"
	sourceNow, err := c.stableListEditState(owner, x.Source)
	if err != nil {
		r.Outcome = "partial"
		return r, err
	}
	if source.ItemsHash != sourceNow.ItemsHash || !sameEditSettings(source, sourceNow, nil) {
		r.Outcome = "partial"
		return r, fmt.Errorf("source changed after preflight; no removal attempted")
	}
	if x.Action == "copy" || len(removeIDs) == 0 {
		r.Stage = "complete"
		r.Verified = true
		r.Outcome = "verified"
		if !r.Attempted {
			r.Outcome = "noop"
		}
		return r, nil
	}
	// A second destination observation is deliberately immediately before source
	// deletion. There is still no server transaction or lock across these lists.
	r.Stage = "destination_recheck"
	destNow, err := c.stableListEditState(owner, x.Destination)
	if err != nil {
		r.Outcome = "partial"
		return r, err
	}
	if destNow.ItemsHash != destAfter.ItemsHash || !sameEditSettings(destAfter, destNow, nil) {
		r.Outcome = "partial"
		return r, fmt.Errorf("destination changed before removal; source removal not attempted")
	}
	r.Stage = "remove"
	r.Attempted = true
	r.RemoveAttempted = true
	result, err := c.ApplyListChange(owner, ListChange{Action: "remove", List: x.Source, Type: x.Type, IDs: removeIDs})
	r.Remove = &result
	if err != nil {
		r.Outcome = "unknown"
		return r, fmt.Errorf("source removal uncertain; do not automatically retry or roll back: %w", err)
	}
	r.Stage = "final_readback"
	finalSource, err := c.stableListEditState(owner, x.Source)
	if err != nil {
		r.Outcome = "unknown"
		return r, err
	}
	finalDest, err := c.stableListEditState(owner, x.Destination)
	if err != nil {
		r.Outcome = "unknown"
		return r, err
	}
	if !transferPreserved(source, finalSource, removed, nil) || !transferPreserved(dest, finalDest, nil, added) {
		r.Outcome = "partial"
		return r, fmt.Errorf("move readback mismatch; inspect both lists, no automatic retry/rollback")
	}
	r.Stage = "complete"
	r.Verified = true
	r.Outcome = "verified"
	return r, nil
}
