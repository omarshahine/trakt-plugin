package api

import "encoding/json"

func ratingRestoreAcknowledged(data []byte, want map[string]int) bool {
	return restoreAddAcknowledged(data, want, false)
}

// Favorites may legitimately report already-existing membership after a race.
// Ratings must never accept an update/overwrite as a verified new addition.
func restoreAddAcknowledged(data []byte, want map[string]int, allowExisting bool) bool {
	var ack struct {
		Added    map[string]int               `json:"added"`
		Existing map[string]int               `json:"existing"`
		Updated  map[string]int               `json:"updated"`
		NotFound map[string][]json.RawMessage `json:"not_found"`
	}
	if json.Unmarshal(data, &ack) != nil || ack.Added == nil || ack.NotFound == nil {
		return false
	}
	for key, count := range ack.Added {
		if count < 0 || count > 100 || count > want[key] {
			return false
		}
	}
	for key, count := range ack.Existing {
		if count < 0 || count > 100 || (!allowExisting && count != 0) || count > want[key] {
			return false
		}
	}
	for key, count := range want {
		if ack.Added[key]+ack.Existing[key] != count {
			return false
		}
	}
	for _, count := range ack.Updated {
		if count != 0 {
			return false
		}
	}
	for _, rows := range ack.NotFound {
		if len(rows) != 0 {
			return false
		}
	}
	return true
}
