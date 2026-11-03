package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
)

// CollectionEntry is an observation, NOT an executable sync payload.
type CollectionEntry struct {
	Type        string                     `json:"type"`
	TraktID     string                     `json:"trakt_id"`
	CollectedAt string                     `json:"collected_at"`
	Metadata    map[string]json.RawMessage `json:"metadata"`
}
type CollectionConflict struct {
	Type    string   `json:"type"`
	TraktID string   `json:"trakt_id"`
	Fields  []string `json:"fields"`
}
type CollectionPlan struct {
	Version       int                  `json:"version"`
	Kind          string               `json:"kind"`
	ReadOnly      bool                 `json:"read_only"`
	Account       api.RestoreAccount   `json:"account"`
	SourceSHA256  string               `json:"source_sha256"`
	CurrentSHA256 string               `json:"current_sha256"`
	Source        CompareInput         `json:"source"`
	Current       CompareInput         `json:"current"`
	Missing       []CollectionEntry    `json:"missing"`
	Conflicts     []CollectionConflict `json:"conflicts"`
	Blocked       []PlanIssue          `json:"blocked"`
	Unchanged     int                  `json:"unchanged"`
	CurrentOnly   int                  `json:"current_only"`
	Warnings      []string             `json:"warnings"`
	ReviewID      string               `json:"review_id"`
}

func collectionEntry(kind string, raw json.RawMessage) (CollectionEntry, error) {
	e := CollectionEntry{Type: kind, Metadata: map[string]json.RawMessage{}}
	var row map[string]json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil || row == nil {
		return e, fmt.Errorf("invalid collection row")
	}
	var media struct {
		IDs struct {
			Trakt json.RawMessage `json:"trakt"`
		} `json:"ids"`
	}
	if err := json.Unmarshal(row[kind], &media); err != nil {
		return e, fmt.Errorf("missing media identity")
	}
	id, err := positiveIdentity(media.IDs.Trakt)
	if err != nil {
		return e, err
	}
	e.TraktID = id
	if explicit, ok := row["type"]; ok {
		var value string
		if json.Unmarshal(explicit, &value) != nil || value != kind {
			return e, fmt.Errorf("conflicting collection type")
		}
	}
	if json.Unmarshal(row["collected_at"], &e.CollectedAt) != nil {
		return e, fmt.Errorf("missing collection date")
	}
	date, err := time.Parse(time.RFC3339Nano, e.CollectedAt)
	if err != nil || date.IsZero() {
		return e, fmt.Errorf("unknown or invalid collection date")
	}
	e.CollectedAt = date.UTC().Format(time.RFC3339Nano)
	if m, ok := row["metadata"]; ok && string(m) != "null" {
		if json.Unmarshal(m, &e.Metadata) != nil || e.Metadata == nil {
			return e, fmt.Errorf("invalid technical metadata")
		}
	}
	return e, nil
}

// PlanCollectionRestore reads immutable local ZIPs only; it never loads a login,
// constructs a sync payload, calls Trakt or offers an apply mode. Flat episode
// IDs are authoritative: nested show summaries must never become whole-show adds.
func PlanCollectionRestore(source, current string) (CollectionPlan, error) {
	r := CollectionPlan{Version: 1, Kind: "collection_observations", ReadOnly: true, Missing: []CollectionEntry{}, Conflicts: []CollectionConflict{}, Blocked: []PlanIssue{}, Warnings: []string{
		"Read-only observations, not an executable restore payload; no apply mode exists.",
		"Only flat movie/episode collections are planned. Nested show summaries are not expanded or reconciled.",
		"Dates and technical metadata are observed, not a guarantee the API will preserve them. Unknown metadata keys are retained for review, not advertised as writable.",
		"updated_at, available_on, provider/media descriptions and show-summary aggregates are outside this plan. Current-only entries are never deletion candidates.",
	}}
	pair, err := readRestoreArchives(source, current)
	if err != nil {
		return r, err
	}
	r.Account = pair.account
	r.SourceSHA256 = pair.sourceHash
	r.CurrentSHA256 = pair.currentHash
	r.Source = pair.source.info
	r.Current = pair.current.info
	for _, kind := range []string{"movie", "episode"} {
		resource := "collection/" + kind + "s"
		sourceRows, sOK := pair.source.data[resource]
		currentRows, cOK := pair.current.data[resource]
		if !sOK || !cOK {
			return r, fmt.Errorf("both archives require %s coverage; cannot infer absence", resource)
		}
		present := map[string]CollectionEntry{}
		for _, raw := range currentRows {
			e, err := collectionEntry(kind, raw)
			if err != nil {
				return r, fmt.Errorf("invalid current %s observation; absence/conflicts cannot be inferred", resource)
			}
			if _, ok := present[e.TraktID]; ok {
				return r, fmt.Errorf("duplicate current collection identity")
			}
			present[e.TraktID] = e
		}
		seen := map[string]bool{}
		for _, id := range sortedKeys(sourceRows) {
			e, err := collectionEntry(kind, sourceRows[id])
			if err != nil {
				r.Blocked = append(r.Blocked, PlanIssue{resource, id, "invalid identity, date or technical metadata; observation withheld"})
				continue
			}
			if seen[e.TraktID] {
				return r, fmt.Errorf("duplicate source collection identity")
			}
			seen[e.TraktID] = true
			other, ok := present[e.TraktID]
			if !ok {
				r.Missing = append(r.Missing, e)
				continue
			}
			fields := []string{}
			if e.CollectedAt != other.CollectedAt {
				fields = append(fields, "collected_at")
			}
			keys := map[string]bool{}
			for k := range e.Metadata {
				keys[k] = true
			}
			for k := range other.Metadata {
				keys[k] = true
			}
			for _, k := range sortedKeys(keys) {
				a, aOK := e.Metadata[k]
				b, bOK := other.Metadata[k]
				var av, bv any
				ad := json.NewDecoder(bytes.NewReader(a))
				ad.UseNumber()
				_ = ad.Decode(&av)
				bd := json.NewDecoder(bytes.NewReader(b))
				bd.UseNumber()
				_ = bd.Decode(&bv)
				if aOK != bOK || !reflect.DeepEqual(av, bv) {
					fields = append(fields, "metadata."+k)
				}
			}
			sort.Strings(fields)
			if len(fields) > 0 {
				r.Conflicts = append(r.Conflicts, CollectionConflict{kind, e.TraktID, fields})
			} else {
				r.Unchanged++
			}
		}
		// A blocked source row still has a validated archive identity. Do not call it
		// current-only or suggest deletion simply because parsing its details failed.
		for id := range present {
			if _, ok := sourceRows[id]; !ok && !seen[id] {
				r.CurrentOnly++
			}
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	hash := sha256.Sum256(encoded)
	r.ReviewID = hex.EncodeToString(hash[:])
	return r, nil
}
