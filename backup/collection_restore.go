package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/api"
)

type CollectionRestoreReview struct {
	Version       int                     `json:"version"`
	Kind          string                  `json:"kind"`
	ReadOnly      bool                    `json:"read_only"`
	Account       api.RestoreAccount      `json:"account"`
	SourceSHA256  string                  `json:"source_sha256"`
	CurrentSHA256 string                  `json:"current_sha256"`
	Limit         int                     `json:"limit"`
	Remaining     int                     `json:"remaining"`
	Collections   []api.RestoreCollection `json:"collections"`
	Blocked       []PlanIssue             `json:"blocked"`
	OutOfScope    []string                `json:"out_of_scope"`
	ReviewID      string                  `json:"review_id"`
}

func PreviewCollectionRestore(source, current string, limit int) (CollectionRestoreReview, error) {
	r := CollectionRestoreReview{Version: 1, Kind: "collection_missing_v1", ReadOnly: true, Limit: limit, Collections: []api.RestoreCollection{}, Blocked: []PlanIssue{}, OutOfScope: []string{"whole_show_expansion", "overwrites", "non_null_3d_hdr", "untested_technical_values"}}
	if limit < 1 || limit > 100 {
		return r, fmt.Errorf("limit must be 1-100")
	}
	pair, err := readRestoreArchives(source, current)
	if err != nil {
		return r, err
	}
	r.Account = pair.account
	r.SourceSHA256 = pair.sourceHash
	r.CurrentSHA256 = pair.currentHash
	for _, kind := range []string{"movie", "episode"} {
		resource := "collection/" + kind + "s"
		a, aOK := pair.source.data[resource]
		b, bOK := pair.current.data[resource]
		if !aOK || !bOK {
			return r, fmt.Errorf("both archives require %s coverage", resource)
		}
		for _, raw := range b {
			if _, err := api.DecodeRestoreCollection(raw, kind); err != nil {
				return r, fmt.Errorf("unsupported current collection scope; no absence inferred")
			}
		}
		for _, id := range sortedKeys(a) {
			x, err := api.DecodeRestoreCollection(a[id], kind)
			if err != nil {
				r.Blocked = append(r.Blocked, PlanIssue{resource, id, "unsupported identity/date/technical metadata"})
				continue
			}
			if raw, ok := b[id]; ok {
				existing, _ := api.DecodeRestoreCollection(raw, kind)
				if existing != x {
					r.Blocked = append(r.Blocked, PlanIssue{resource, id, "existing date/metadata conflict; never overwrite"})
				}
				continue
			}
			if len(r.Collections) == limit {
				r.Remaining++
				continue
			}
			r.Collections = append(r.Collections, x)
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
