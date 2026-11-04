package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ImportItem follows Trakt's website JSON importer, not the sync API payload.
// Actions stay separate: combining fields could repeat a watch or change a rating.
type ImportItem struct {
	TraktID       string `json:"trakt_id"`
	Type          string `json:"type"`
	WatchedAt     string `json:"watched_at,omitempty"`
	WatchlistedAt string `json:"watchlisted_at,omitempty"`
	Rating        int    `json:"rating,omitempty"`
	RatedAt       string `json:"rated_at,omitempty"`
}

type PlanIssue struct {
	Resource string `json:"resource"`
	ID       string `json:"id,omitempty"`
	Reason   string `json:"reason"`
}

type PlanCandidate struct {
	Resource string     `json:"resource"`
	ID       string     `json:"id"`
	Item     ImportItem `json:"item"`
}

type RestorePlan struct {
	Version       int             `json:"version"`
	ReadOnly      bool            `json:"read_only"`
	AccountMatch  string          `json:"account_match"`
	Source        CompareInput    `json:"source"`
	Current       CompareInput    `json:"current"`
	Candidates    []PlanCandidate `json:"candidates"`
	Conflicts     []PlanIssue     `json:"conflicts"`
	Unsupported   []PlanIssue     `json:"unsupported"`
	Warnings      []string        `json:"warnings"`
	HistoryPolicy string          `json:"history_policy"`
}

// PlanRestore validates both archives and plans additions only. Current is a
// snapshot, not a live lock: candidates must be reviewed and rechecked before use.
// No credentials, network, account writes or deletion operations are involved.
func PlanRestore(source, current string) (RestorePlan, error) {
	a, err := readComparisonArchive(source)
	if err != nil {
		return RestorePlan{}, fmt.Errorf("source archive: %w", err)
	}
	b, err := readComparisonArchive(current)
	if err != nil {
		return RestorePlan{}, fmt.Errorf("current archive: %w", err)
	}
	return planRestoreArchives(a, b)
}

func planRestoreArchives(a, b *comparisonArchive) (RestorePlan, error) {
	p := RestorePlan{Version: 1, ReadOnly: true, Candidates: []PlanCandidate{}, Conflicts: []PlanIssue{}, Unsupported: []PlanIssue{}, Warnings: []string{
		"Preview only; current means the supplied snapshot. Refresh it immediately before any manual import. No account changes were made.",
		"Import JSON covers missing movie/episode watches, watchlist entries and ratings only. It cannot restore all backup categories, notes/ranks, or history event IDs. Missing rating timestamps are omitted, not reconstructed.",
		"Website-layout archive completeness is unknown. Historical supplemental account fields are not restore candidates.",
		"Trakt website import acceptance and duplicate-watch handling have not been live-tested. Review conflicts separately; no conflict is included in import JSON.",
	}}
	aID, err := comparisonAccount(a)
	if err != nil {
		return p, err
	}
	bID, err := comparisonAccount(b)
	if err != nil {
		return p, err
	}
	matched, err := matchComparisonAccount(aID, bID)
	if err != nil {
		return p, err
	}
	if !matched {
		return p, fmt.Errorf("restore planning requires matching account identities in both archives")
	}
	p.AccountMatch, p.Source, p.Current = "matched", a.info, b.info
	p.HistoryPolicy = "unknown"
	if b.watchOnlyOnce != nil {
		p.HistoryPolicy = "multiple_plays_allowed"
		if *b.watchOnlyOnce {
			p.HistoryPolicy = "watch_only_once"
		}
	}
	if p.HistoryPolicy != "multiple_plays_allowed" {
		p.Warnings = append(p.Warnings, "Repeat-watch candidates are withheld: current snapshot browsing.watch_only_once is true or unverified. Verify Disable Multiple Plays in the official Trakt settings and take a fresh snapshot; this planner never changes account preferences.")
	}
	for _, resource := range sortedKeys(a.data) {
		rows := a.data[resource]
		currentRows, covered := b.data[resource]
		if !covered {
			p.Unsupported = append(p.Unsupported, PlanIssue{Resource: resource, Reason: "current snapshot lacks coverage; absence cannot be inferred"})
			continue
		}
		supported := resource == "history" || resource == "watchlist" || strings.HasPrefix(resource, "ratings/")
		list := strings.HasPrefix(resource, "lists/")
		if !supported && !list {
			p.Unsupported = append(p.Unsupported, PlanIssue{Resource: resource, Reason: "not supported by this restore planner"})
			continue
		}
		// Index by media identity, not list-entry IDs or historical event IDs.
		index := map[string][]json.RawMessage{}
		invalidCurrent := false
		for _, id := range sortedKeys(currentRows) {
			key, _, e := planMedia(resource, currentRows[id])
			if e != nil {
				invalidCurrent = true
				break
			}
			index[key] = append(index[key], currentRows[id])
		}
		if invalidCurrent {
			p.Conflicts = append(p.Conflicts, PlanIssue{Resource: resource, Reason: "current media identity is unsupported or invalid; entire resource withheld"})
			continue
		}
		usedWatches := map[string]int{}
		sourceCounts := map[string]int{}
		sourceMediaCounts := map[string]int{}
		for _, row := range rows {
			key, _, e := planMedia(resource, row)
			if e != nil {
				continue
			}
			if resource == "history" {
				sourceMediaCounts[key]++
				date, e := planDate(row, "watched_at", true)
				if e != nil {
					continue
				}
				key += "/" + date
			}
			sourceCounts[key]++
		}
		for _, id := range sortedKeys(rows) {
			row := rows[id]
			key, item, e := planMedia(resource, row)
			if e != nil {
				p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "missing or unsupported media identity"})
				continue
			}
			if resource == "history" {
				if item.Type != "movie" && item.Type != "episode" {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "show/season watches are ambiguous; not expanded into episodes"})
					continue
				}
				date, e := planDate(row, "watched_at", true)
				if e != nil {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "invalid or absent watch date; not silently converted to unknown"})
					continue
				}
				item.WatchedAt = date
				if existing, ok := currentRows[id]; ok {
					existingKey, _, _ := planMedia(resource, existing)
					existingDate, dateErr := planDate(existing, "watched_at", true)
					if dateErr != nil || existingKey != key || existingDate != date {
						p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "same watch-event ID has conflicting media or date"})
						continue
					}
				}
				signature := key + "/" + date
				same := 0
				ambiguous := false
				for _, existing := range index[key] {
					d, e := planDate(existing, "watched_at", true)
					if e != nil {
						ambiguous = true
						break
					}
					if d == date {
						same++
					}
					if (date == "unknown" || d == "unknown") && date != d {
						ambiguous = true
					}
				}
				usedWatches[signature]++
				if usedWatches[signature] <= same {
					continue
				}
				if ambiguous || sourceCounts[signature] > 1 {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "ambiguous unknown-date or identical-time repeat watch; manual review required"})
					continue
				}
				if p.HistoryPolicy != "multiple_plays_allowed" && (len(index[key]) > 0 || sourceMediaCounts[key] > 1) {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "repeat watch withheld: browsing.watch_only_once is enabled or unverified; import could replace an existing watch instead of adding one"})
					continue
				}
			} else if strings.HasPrefix(resource, "ratings/") {
				object, _ := decodeObject(row)
				if e := json.Unmarshal(object["rating"], &item.Rating); e != nil || item.Rating < 1 || item.Rating > 10 {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "rating must be an integer from 1 to 10"})
					continue
				}
				item.RatedAt, e = planDate(row, "rated_at", false)
				if e != nil {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "invalid rating timestamp"})
					continue
				}
				if existing := index[key]; len(existing) > 0 {
					obj, _ := decodeObject(existing[0])
					var rating int
					if json.Unmarshal(obj["rating"], &rating) != nil || rating != item.Rating {
						p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "current rating differs; never overwrite automatically"})
					}
					continue
				}
			} else {
				if len(index[key]) > 0 {
					continue
				}
				if sourceCounts[key] > 1 {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "duplicate source media entries require manual review"})
					continue
				}
				if list {
					p.Unsupported = append(p.Unsupported, PlanIssue{resource, id, "missing custom-list item; website JSON import has no target-list field; notes/rank require separate handling"})
					continue
				}
				item.WatchlistedAt, e = planDate(row, "listed_at", true)
				if e != nil {
					p.Conflicts = append(p.Conflicts, PlanIssue{resource, id, "watchlist date missing or invalid; no fabricated timestamp"})
					continue
				}
			}
			p.Candidates = append(p.Candidates, PlanCandidate{resource, id, item})
		}
	}
	return p, nil
}

func planMedia(resource string, raw json.RawMessage) (string, ImportItem, error) {
	var item ImportItem
	obj, err := decodeObject(raw)
	if err != nil {
		return "", item, err
	}
	kind := ""
	if strings.HasPrefix(resource, "ratings/") {
		kind = strings.TrimSuffix(strings.TrimPrefix(resource, "ratings/"), "s")
	} else {
		_ = json.Unmarshal(obj["type"], &kind)
	}
	if kind == "" {
		for _, k := range []string{"movie", "episode", "season", "show"} {
			if _, ok := obj[k]; ok {
				kind = k
				break
			}
		}
	}
	switch kind {
	case "movie", "episode", "season", "show":
	default:
		return "", item, fmt.Errorf("unsupported media type")
	}
	media, err := decodeObject(obj[kind])
	if err != nil {
		return "", item, err
	}
	ids, err := decodeObject(media["ids"])
	if err != nil {
		return "", item, err
	}
	id, err := positiveIdentity(ids["trakt"])
	if err != nil {
		return "", item, err
	}
	item.Type, item.TraktID = kind, id
	return kind + ":" + id, item, nil
}

func planDate(raw json.RawMessage, field string, required bool) (string, error) {
	obj, err := decodeObject(raw)
	if err != nil {
		return "", err
	}
	value, exists := obj[field]
	if !exists {
		if !required {
			return "", nil
		}
		return "", fmt.Errorf("date required")
	}
	var text string
	if json.Unmarshal(value, &text) != nil {
		return "", fmt.Errorf("date must be a string")
	}
	if field == "watched_at" && text == "unknown" {
		return text, nil
	}
	date, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return "", fmt.Errorf("invalid ISO8601 date")
	}
	return date.UTC().Format(time.RFC3339Nano), nil
}

// WriteImportJSON writes a new private file only. It does not call the importer.
func WriteImportJSON(filename string, p RestorePlan) error {
	if p.Version != 1 || !p.ReadOnly || p.AccountMatch != "matched" {
		return fmt.Errorf("not a validated restore plan")
	}
	items := make([]ImportItem, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		item := candidate.Item
		if n, err := strconv.ParseInt(item.TraktID, 10, 64); err != nil || n <= 0 {
			return fmt.Errorf("invalid import ID")
		}
		switch item.Type {
		case "movie", "show", "season", "episode":
		default:
			return fmt.Errorf("invalid import type")
		}
		actions := 0
		if item.WatchedAt != "" {
			actions++
			if item.Type != "movie" && item.Type != "episode" {
				return fmt.Errorf("ambiguous watch type")
			}
			if item.WatchedAt != "unknown" {
				if _, err := time.Parse(time.RFC3339Nano, item.WatchedAt); err != nil {
					return fmt.Errorf("invalid watch timestamp")
				}
			}
		}
		if item.WatchlistedAt != "" {
			actions++
			if _, err := time.Parse(time.RFC3339Nano, item.WatchlistedAt); err != nil {
				return fmt.Errorf("invalid watchlist timestamp")
			}
		}
		if item.Rating != 0 {
			actions++
			if item.Rating < 1 || item.Rating > 10 {
				return fmt.Errorf("invalid import rating")
			}
		}
		if item.RatedAt != "" {
			if item.Rating == 0 {
				return fmt.Errorf("rated_at without rating")
			}
			if _, err := time.Parse(time.RFC3339Nano, item.RatedAt); err != nil {
				return fmt.Errorf("invalid rating timestamp")
			}
		}
		if actions != 1 {
			return fmt.Errorf("import candidate must contain exactly one action")
		}
		items = append(items, item)
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(items)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
