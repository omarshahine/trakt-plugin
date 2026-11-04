package backup

import (
	"bytes"
	"encoding/json"
)

// Missing, null and malformed values are unknown, never silently false. For
// supplemented archives prefer the raw API snapshot over historical fields.
func archiveWatchOnlyOnce(a *comparisonArchive) *bool {
	raw, ok := a.data["raw-api/user/settings"]["snapshot"]
	if !ok {
		raw = a.data["user/settings"]["snapshot"]
	}
	obj, err := decodeObject(raw)
	if err != nil {
		return nil
	}
	browsing, err := decodeObject(obj["browsing"])
	if err != nil {
		return nil
	}
	value := bytes.TrimSpace(browsing["watch_only_once"])
	if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
		return nil
	}
	var once bool
	if json.Unmarshal(value, &once) != nil {
		return nil
	}
	return &once
}
