package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
)

// Full history determines whether any episode of a show has been watched. It
// does not pretend a partially watched show is an entirely unseen suggestion.
func (c *APIClient) discoveryExclusions(o DiscoveryOptions) (map[string]bool, map[string]bool, string, error) {
	// Filtering is read-only even when the access token has expired.
	reader := *c
	reader.Credentials.RefreshToken = ""
	c = &reader
	watched, watchlisted := map[string]bool{}, map[string]bool{}
	if !o.ExcludeWatched && !o.ExcludeWatchlisted {
		return watched, watchlisted, "", nil
	}
	owner, err := c.ResolveListOwner()
	if err != nil {
		return nil, nil, "", err
	}
	start, err := c.GetBackupActivity()
	if err != nil {
		return nil, nil, "", err
	}
	if o.ExcludeWatched {
		events := map[string]bool{}
		err = c.walkStrictRecords("/users/"+url.PathEscape(owner)+"/history", 1000, 100000, func(raw json.RawMessage) error {
			var row struct {
				ID    json.Number `json:"id"`
				Type  string      `json:"type"`
				Movie struct {
					IDs struct {
						Trakt json.Number `json:"trakt"`
					} `json:"ids"`
				} `json:"movie"`
				Show struct {
					IDs struct {
						Trakt json.Number `json:"trakt"`
					} `json:"ids"`
				} `json:"show"`
				Episode struct {
					IDs struct {
						Trakt json.Number `json:"trakt"`
					} `json:"ids"`
				} `json:"episode"`
			}
			if json.Unmarshal(raw, &row) != nil {
				return fmt.Errorf("invalid history row")
			}
			event := row.ID.String()
			if _, err := listID(event); err != nil {
				return err
			}
			if events[event] {
				return fmt.Errorf("duplicate history event")
			}
			events[event] = true
			switch row.Type {
			case "movie":
				id := row.Movie.IDs.Trakt.String()
				if _, err := listID(id); err != nil {
					return err
				}
				if o.Type == "movie" {
					watched[id] = true
				}
			case "episode":
				if _, err := listID(row.Episode.IDs.Trakt.String()); err != nil {
					return fmt.Errorf("invalid episode history identity")
				}
				id := row.Show.IDs.Trakt.String()
				if _, err := listID(id); err != nil {
					return fmt.Errorf("episode history missing parent show identity")
				}
				if o.Type == "show" {
					watched[id] = true
				}
			default:
				return fmt.Errorf("unsupported history type; cannot claim complete exclusions")
			}
			return nil
		})
		if err != nil {
			return nil, nil, "", err
		}
	}
	if o.ExcludeWatchlisted {
		entries := map[string]bool{}
		err = c.walkStrictRecords("/users/"+url.PathEscape(owner)+"/watchlist", 1000, 100000, func(raw json.RawMessage) error {
			var row map[string]json.RawMessage
			if json.Unmarshal(raw, &row) != nil || row == nil {
				return fmt.Errorf("invalid watchlist row")
			}
			var kind string
			if json.Unmarshal(row["type"], &kind) != nil {
				return fmt.Errorf("missing watchlist type")
			}
			if kind == "episode" || kind == "season" {
				var target struct {
					IDs struct {
						Trakt json.Number `json:"trakt"`
					} `json:"ids"`
				}
				if json.Unmarshal(row[kind], &target) != nil {
					return fmt.Errorf("invalid watchlist target")
				}
				if _, err := listID(target.IDs.Trakt.String()); err != nil {
					return err
				}
			}
			mediaKind := kind
			if kind == "episode" || kind == "season" {
				mediaKind = "show"
			}
			if mediaKind != "movie" && mediaKind != "show" {
				return fmt.Errorf("unsupported watchlist type")
			}
			var media struct {
				IDs struct {
					Trakt json.Number `json:"trakt"`
				} `json:"ids"`
			}
			if json.Unmarshal(row[mediaKind], &media) != nil {
				return fmt.Errorf("missing watchlist media")
			}
			id := media.IDs.Trakt.String()
			if _, err := listID(id); err != nil {
				return err
			}
			var entry json.Number
			if json.Unmarshal(row["id"], &entry) != nil {
				return fmt.Errorf("missing watchlist entry ID")
			}
			if _, err := listID(entry.String()); err != nil {
				return err
			}
			if entries[entry.String()] {
				return fmt.Errorf("duplicate watchlist entry")
			}
			entries[entry.String()] = true
			if mediaKind == o.Type {
				watchlisted[id] = true
			}
			return nil
		})
		if err != nil {
			return nil, nil, "", err
		}
	}
	end, err := c.GetBackupActivity()
	if err != nil {
		return nil, nil, "", err
	}
	if !bytes.Equal(start, end) {
		return nil, nil, "", fmt.Errorf("account activity changed during exclusion reads")
	}
	return watched, watchlisted, owner, nil
}
