package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/omarshahine/trakt-plugin/api"
)

type RatingRestoreReview struct {
	Version       int                      `json:"version"`
	ReadOnly      bool                     `json:"read_only"`
	Account       api.RatingRestoreAccount `json:"account"`
	SourceSHA256  string                   `json:"source_sha256"`
	CurrentSHA256 string                   `json:"current_sha256"`
	Limit         int                      `json:"limit"`
	Remaining     int                      `json:"remaining"`
	Ratings       []api.RestoreRating      `json:"ratings"`
	Blocked       []PlanIssue              `json:"blocked"`
	ReviewID      string                   `json:"review_id"`
}

// PreviewRatingRestore parses immutable in-memory archive snapshots. Its digest
// binds the exact bytes, account, selected batch, limit and withheld candidates.
// No credentials, network or account changes are involved.
func PreviewRatingRestore(source, current string, limit int) (RatingRestoreReview, error) {
	r := RatingRestoreReview{Version: 1, ReadOnly: true, Limit: limit, Ratings: []api.RestoreRating{}, Blocked: []PlanIssue{}}
	if limit < 1 || limit > 100 {
		return r, fmt.Errorf("limit must be 1-100 ratings per reviewed batch")
	}
	pair, err := readRestoreArchives(source, current)
	if err != nil {
		return r, err
	}
	p, err := planRestoreArchives(pair.source, pair.current)
	if err != nil {
		return r, err
	}
	r.Account = pair.account
	r.SourceSHA256, r.CurrentSHA256 = pair.sourceHash, pair.currentHash
	for _, issue := range append(p.Conflicts, p.Unsupported...) {
		if strings.HasPrefix(issue.Resource, "ratings/") {
			r.Blocked = append(r.Blocked, issue)
		}
	}
	for _, candidate := range p.Candidates {
		if !strings.HasPrefix(candidate.Resource, "ratings/") {
			continue
		}
		x := candidate.Item
		if x.RatedAt == "" {
			r.Blocked = append(r.Blocked, PlanIssue{candidate.Resource, candidate.ID, "rating timestamp missing; executor never fabricates a date"})
			continue
		}
		if len(r.Ratings) == limit {
			r.Remaining++
			continue
		}
		r.Ratings = append(r.Ratings, api.RestoreRating{Type: x.Type, TraktID: x.TraktID, Rating: x.Rating, RatedAt: x.RatedAt})
	}
	// Hash before ReviewID is set; the empty field is a stable part of v1.
	encoded, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	digest := sha256.Sum256(encoded)
	r.ReviewID = hex.EncodeToString(digest[:])
	return r, nil
}
