package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/omarshahine/trakt-plugin/api"
)

type FavoriteRestoreReview struct {
	Version       int                   `json:"version"`
	Kind          string                `json:"kind"`
	ReadOnly      bool                  `json:"read_only"`
	Account       api.RestoreAccount    `json:"account"`
	SourceSHA256  string                `json:"source_sha256"`
	CurrentSHA256 string                `json:"current_sha256"`
	Limit         int                   `json:"limit"`
	Remaining     int                   `json:"remaining"`
	Favorites     []api.RestoreFavorite `json:"favorites"`
	Blocked       []PlanIssue           `json:"blocked"`
	OutOfScope    []string              `json:"out_of_scope"`
	ReviewID      string                `json:"review_id"`
}

func PreviewFavoriteRestore(source, current string, limit int) (FavoriteRestoreReview, error) {
	r := FavoriteRestoreReview{Version: 1, Kind: "favorites_membership", ReadOnly: true, Limit: limit, Favorites: []api.RestoreFavorite{}, Blocked: []PlanIssue{}, OutOfScope: []string{"ranks", "notes", "timestamps"}}
	if limit < 1 || limit > 100 {
		return r, fmt.Errorf("limit must be 1-100 favorites per reviewed batch")
	}
	pair, err := readRestoreArchives(source, current)
	if err != nil {
		return r, err
	}
	sourceRows, sourceCovered := pair.source.data["favorites"]
	currentRows, currentCovered := pair.current.data["favorites"]
	if !sourceCovered || !currentCovered {
		return r, fmt.Errorf("both archives must contain favorites coverage; absence cannot be inferred")
	}
	r.Account = pair.account
	r.SourceSHA256, r.CurrentSHA256 = pair.sourceHash, pair.currentHash
	present := map[string]bool{}
	for _, raw := range currentRows {
		x, err := api.FavoriteMembership(raw)
		if err != nil {
			return r, fmt.Errorf("current favorites identity/type is unsupported; no absence inferred")
		}
		key := x.Type + ":" + x.TraktID
		if present[key] {
			return r, fmt.Errorf("duplicate current favorite membership")
		}
		present[key] = true
	}
	seen := map[string]bool{}
	for _, id := range sortedKeys(sourceRows) {
		x, err := api.FavoriteMembership(sourceRows[id])
		if err != nil {
			r.Blocked = append(r.Blocked, PlanIssue{"favorites", id, "unsupported or invalid favorite membership; movie/show IDs required"})
			continue
		}
		key := x.Type + ":" + x.TraktID
		if seen[key] {
			return r, fmt.Errorf("duplicate source favorite membership")
		}
		seen[key] = true
		if present[key] {
			continue
		}
		if len(r.Favorites) == limit {
			r.Remaining++
			continue
		}
		r.Favorites = append(r.Favorites, x)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	hash := sha256.Sum256(encoded)
	r.ReviewID = hex.EncodeToString(hash[:])
	return r, nil
}
