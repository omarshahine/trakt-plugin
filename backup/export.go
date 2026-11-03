// Package backup creates scoped, verifiable ZIP exports without changing Trakt.
package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
)

type Source interface {
	GetBackupPage(string, string, string, api.PaginationsParams) ([]json.RawMessage, api.Pagination, error)
	GetBackupActivity() (json.RawMessage, error)
}

type Options struct {
	User     string
	Output   string
	Limit    int
	Delay    time.Duration
	MaxPages int
	// Progress receives observed counts after a page is safely added to the ZIP.
	Progress func(Resource)
}

type File struct {
	Path    string `json:"path"`
	Records int    `json:"records"`
	SHA256  string `json:"sha256"`
}
type Resource struct {
	Name     string `json:"name"`
	Expected int    `json:"expected"`
	Records  int    `json:"records"`
	Pages    int    `json:"pages"`
	Complete bool   `json:"complete"`
}
type Manifest struct {
	Version     int        `json:"version"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  time.Time  `json:"finished_at"`
	Scopes      []string   `json:"scopes"`
	Excluded    []string   `json:"excluded"`
	Complete    bool       `json:"complete"`
	Consistency string     `json:"consistency"`
	Files       []File     `json:"files"`
	Resources   []Resource `json:"resources"`
	Errors      []string   `json:"errors"`
}

type exporter struct {
	source      Source
	options     Options
	zip         *zip.Writer
	manifest    *Manifest
	lastRequest time.Time
}

// Export refuses to overwrite an existing path. A failed export retains a ZIP
// with complete=false and details of completed pages when storage permits.
// Completeness is scoped to history/watchlist/custom lists, not the website's
// full account export. An unchanged activity marker is evidence of consistency,
// not a transactional snapshot guarantee from Trakt.
func Export(source Source, o Options) (m Manifest, err error) {
	if o.User == "" || o.Output == "" || o.Limit < 1 || o.Limit > 1000 || o.Delay < 0 || o.MaxPages < 1 {
		return m, fmt.Errorf("user/output required; limit 1-1000, nonnegative delay and positive max-pages required")
	}
	f, err := os.OpenFile(o.Output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return m, err
	}
	m = Manifest{Version: 1, StartedAt: time.Now().UTC(), Scopes: []string{"history", "watchlist", "custom_lists"},
		Excluded:    []string{"ratings", "collections", "favorites", "comments", "notes_outside_lists", "hidden_items", "settings", "social", "collaborated_lists_owned_by_others"},
		Consistency: "unverified", Files: []File{}, Resources: []Resource{}, Errors: []string{}}
	x := exporter{source: source, options: o, zip: zip.NewWriter(f), manifest: &m}
	workErr := x.run()
	if workErr != nil {
		m.Errors = append(m.Errors, workErr.Error())
	}
	m.Complete = workErr == nil
	m.FinishedAt = time.Now().UTC()
	data, encodeErr := json.MarshalIndent(m, "", "  ")
	if encodeErr == nil {
		var wErr error
		w, wErr := x.zip.Create("manifest.json")
		encodeErr = wErr
		if wErr == nil {
			_, encodeErr = w.Write(append(data, '\n'))
		}
	}
	zipErr := x.zip.Close()
	syncErr := f.Sync()
	closeErr := f.Close()
	for _, e := range []error{encodeErr, zipErr, syncErr, closeErr} {
		if e != nil {
			m.Complete = false
			return m, fmt.Errorf("write backup: %w", e)
		}
	}
	return m, workErr
}

func (x *exporter) wait() {
	if !x.lastRequest.IsZero() {
		if remaining := x.options.Delay - time.Since(x.lastRequest); remaining > 0 {
			time.Sleep(remaining)
		}
	}
	x.lastRequest = time.Now()
}

func (x *exporter) run() error {
	x.wait()
	before, err := x.source.GetBackupActivity()
	if err != nil {
		return fmt.Errorf("initial activity marker: %w", err)
	}
	for _, resource := range []string{"history", "watchlist"} {
		if _, err := x.collect(resource, "", -1); err != nil {
			return err
		}
	}
	lists, err := x.collect("lists", "", -1)
	if err != nil {
		return err
	}
	for _, raw := range lists {
		var list struct {
			IDs struct {
				Trakt int64 `json:"trakt"`
			} `json:"ids"`
			ItemCount *int `json:"item_count"`
		}
		if err := json.Unmarshal(raw, &list); err != nil || list.IDs.Trakt <= 0 || list.ItemCount == nil || *list.ItemCount < 0 {
			return fmt.Errorf("custom list metadata lacks valid ID/item_count")
		}
		if _, err := x.collect("list_items", strconv.FormatInt(list.IDs.Trakt, 10), *list.ItemCount); err != nil {
			return err
		}
	}
	x.wait()
	after, err := x.source.GetBackupActivity()
	if err != nil {
		return fmt.Errorf("final activity marker: %w", err)
	}
	if !bytes.Equal(before, after) {
		x.manifest.Consistency = "changed_during_export"
		return fmt.Errorf("account activity changed during export; retry for a stable backup")
	}
	x.manifest.Consistency = "activity_marker_unchanged"
	return nil
}

func (x *exporter) collect(resource, listID string, metadataCount int) ([]json.RawMessage, error) {
	name := resource
	dir := resource
	if listID != "" {
		name = "list:" + listID
		dir = "lists/" + listID
	}
	index := len(x.manifest.Resources)
	x.manifest.Resources = append(x.manifest.Resources, Resource{Name: name, Expected: -1})
	seen := map[string]bool{}
	var inventory []json.RawMessage
	pageCount, total, reportedLimit := 0, 0, 0
	for page := 1; page <= x.options.MaxPages; page++ {
		x.wait()
		records, p, err := x.source.GetBackupPage(x.options.User, resource, listID, api.PaginationsParams{Page: page, Limit: x.options.Limit})
		if err != nil {
			return nil, fmt.Errorf("%s page %d: %w", name, page, err)
		}
		values := []string{p.Page, p.Limit, p.PageCount, p.ItemCount}
		nums := make([]int, 4)
		for i, v := range values {
			n, e := strconv.Atoi(v)
			if e != nil || n < 0 {
				return nil, fmt.Errorf("%s page %d: missing/invalid pagination headers", name, page)
			}
			nums[i] = n
		}
		if nums[0] != page || nums[1] < 1 || nums[2] < 1 && nums[3] > 0 {
			return nil, fmt.Errorf("%s page %d: inconsistent pagination", name, page)
		}
		pages := (nums[3] + nums[1] - 1) / nums[1]
		if pages == 0 {
			pages = 1
		}
		// Trakt may report either zero or one pages for an empty collection.
		if nums[3] != 0 && pages != nums[2] || nums[3] == 0 && nums[2] > 1 {
			return nil, fmt.Errorf("%s: page count does not match total", name)
		}
		if page == 1 {
			pageCount, total, reportedLimit = pages, nums[3], nums[1]
			x.manifest.Resources[index].Expected = total
			if metadataCount >= 0 && total != metadataCount {
				return nil, fmt.Errorf("%s: item count differs from list metadata", name)
			}
		} else if pages != pageCount || (resource != "history" && nums[3] != total) || nums[1] != reportedLimit {
			return nil, fmt.Errorf("%s page %d: pagination changed during export (total %d -> %d, limit %d -> %d, pages %d -> %d)", name, page, total, nums[3], reportedLimit, nums[1], pageCount, pages)
		}
		expected := reportedLimit
		if page == pageCount {
			// History's earlier pages can carry a stale item count even when
			// the records and activity markers are stable. Reconcile against
			// the terminal page, without relaxing page shape or ID checks.
			if resource == "history" {
				total = nums[3]
				x.manifest.Resources[index].Expected = total
			}
			expected = total - (page-1)*reportedLimit
			if x.manifest.Resources[index].Records+len(records) != total {
				return nil, fmt.Errorf("%s: terminal total does not match collected records", name)
			}
		}
		if len(records) != expected {
			return nil, fmt.Errorf("%s page %d: got %d records, expected %d", name, page, len(records), expected)
		}
		for _, raw := range records {
			var record struct {
				ID  json.Number `json:"id"`
				IDs struct {
					Trakt json.Number `json:"trakt"`
				} `json:"ids"`
			}
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil, fmt.Errorf("%s: invalid record", name)
			}
			id := record.ID.String()
			if resource == "lists" {
				id = record.IDs.Trakt.String()
			}
			if id == "" || seen[id] {
				return nil, fmt.Errorf("%s: missing or duplicate record ID (unstable pagination)", name)
			}
			seen[id] = true
		}
		if records == nil {
			records = []json.RawMessage{}
		}
		data, err := json.MarshalIndent(records, "", "  ")
		if err != nil {
			return nil, err
		}
		data = append(data, '\n')
		path := fmt.Sprintf("%s/page-%06d.json", dir, page)
		w, err := x.zip.Create(path)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(data); err != nil {
			return nil, err
		}
		digest := sha256.Sum256(data)
		x.manifest.Files = append(x.manifest.Files, File{Path: path, Records: len(records), SHA256: hex.EncodeToString(digest[:])})
		x.manifest.Resources[index].Records += len(records)
		x.manifest.Resources[index].Pages++
		if page == pageCount {
			x.manifest.Resources[index].Complete = true
		}
		if x.options.Progress != nil {
			x.options.Progress(x.manifest.Resources[index])
		}
		if resource == "lists" {
			inventory = append(inventory, records...)
		}
		if page == pageCount {
			x.manifest.Resources[index].Complete = true
			return inventory, nil
		}
	}
	return nil, fmt.Errorf("%s: max-pages limit reached; export incomplete", name)
}
