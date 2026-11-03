package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

type RestoreAccount struct {
	Slug string `json:"slug"`
	UUID string `json:"uuid"`
}

// Preserve the original public ratings API name while sharing the same strong
// identity contract with other narrowly scoped restore executors.
type RatingRestoreAccount = RestoreAccount

func (c *APIClient) verifyRestoreAccount(expected string, account RestoreAccount) (json.RawMessage, error) {
	if expected == "" || expected != account.Slug || strings.TrimSpace(account.UUID) == "" {
		return nil, fmt.Errorf("expected account slug and archive UUID are required")
	}
	settings, err := c.GetBackupDocument("me", "user/settings")
	if err != nil {
		return nil, err
	}
	var identity struct {
		User struct {
			IDs struct {
				Slug string `json:"slug"`
				UUID string `json:"uuid"`
			} `json:"ids"`
		} `json:"user"`
	}
	if json.Unmarshal(settings, &identity) != nil || identity.User.IDs.Slug != expected || identity.User.IDs.UUID != account.UUID {
		return nil, fmt.Errorf("live account slug/UUID do not match the reviewed archives; no write attempted")
	}
	return settings, nil
}
