package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	a, sourceHash, err := readRatingRestoreArchive(source)
	if err != nil {
		return r, fmt.Errorf("source: %w", err)
	}
	b, currentHash, err := readRatingRestoreArchive(current)
	if err != nil {
		return r, fmt.Errorf("current: %w", err)
	}
	p, err := planRestoreArchives(a, b)
	if err != nil {
		return r, err
	}
	aID, err := comparisonAccount(a)
	if err != nil {
		return r, err
	}
	bID, err := comparisonAccount(b)
	if err != nil {
		return r, err
	}
	if aID.uuid == "" || bID.uuid == "" || aID.uuid != bID.uuid || strings.TrimSpace(bID.slug) == "" {
		return r, fmt.Errorf("ratings restoration requires matching UUIDs in both archives and a current account slug")
	}
	r.Account = api.RatingRestoreAccount{Slug: bID.slug, UUID: bID.uuid}
	r.SourceSHA256, r.CurrentSHA256 = sourceHash, currentHash
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

func readRatingRestoreArchive(filename string) (*comparisonArchive, string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, "", fmt.Errorf("cannot open archive")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCompareArchive {
		return nil, "", fmt.Errorf("archive must be a regular file within size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCompareArchive+1))
	if err != nil || len(data) > maxCompareArchive {
		return nil, "", fmt.Errorf("archive read/size failure")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", fmt.Errorf("cannot parse ZIP")
	}
	a, err := readComparisonZIP(z)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	return a, hex.EncodeToString(hash[:]), nil
}
