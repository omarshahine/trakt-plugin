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
	"strings"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
)

type Source interface {
	GetBackupPage(string, string, string, api.PaginationsParams) ([]json.RawMessage, api.Pagination, error)
	GetBackupActivity() (json.RawMessage, error)
	GetBackupDocument(string, string) (json.RawMessage, error)
}

type Options struct {
	User              string
	Output            string
	Limit             int
	Delay             time.Duration
	MaxPages          int
	WebsiteSupplement *WebsiteSupplement
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
	Mode        string                `json:"mode"`
	Supplement  *SupplementProvenance `json:"supplement,omitempty"`
	Version     int                   `json:"version"`
	StartedAt   time.Time             `json:"started_at"`
	FinishedAt  time.Time             `json:"finished_at"`
	Scopes      []string              `json:"scopes"`
	Excluded    []string              `json:"excluded"`
	Complete    bool                  `json:"complete"`
	Consistency string                `json:"consistency"`
	Files       []File                `json:"files"`
	Resources   []Resource            `json:"resources"`
	Errors      []string              `json:"errors"`
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
// Completeness is scoped to the manifest's resource categories, not the website's
// full account export. An unchanged activity marker is evidence of consistency,
// not a transactional snapshot guarantee from Trakt.
func Export(source Source, o Options) (m Manifest, err error) {
	if o.User == "" || o.Output == "" || o.Limit < 1 || o.Limit > 1000 || o.Delay < 0 || o.MaxPages < 1 {
		return m, fmt.Errorf("user/output required; limit 1-1000, nonnegative delay and positive max-pages required")
	}
	if o.WebsiteSupplement != nil {
		if err := o.WebsiteSupplement.validate(); err != nil {
			return m, err
		}
		if o.User != o.WebsiteSupplement.Slug {
			return m, fmt.Errorf("supplement is for a different account")
		}
	}
	f, err := os.OpenFile(o.Output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return m, err
	}
	m = Manifest{Version: 1, StartedAt: time.Now().UTC(), Scopes: []string{"history", "watchlist", "custom_lists", "ratings", "collections", "favorites", "account_snapshots", "saved_filters", "playback", "watched_summaries", "hidden_items", "comments_including_replies", "notes", "social", "likes", "collaborated_list_metadata"},
		Excluded:    []string{"collaborated_list_contents_owned_by_others", "website_fields_not_returned_by_api"},
		Consistency: "unverified", Files: []File{}, Resources: []Resource{}, Errors: []string{}}
	m.Mode = "api_only"
	if o.WebsiteSupplement != nil {
		m.Mode = "website_supplemented"
		m.Supplement = &SupplementProvenance{Source: o.WebsiteSupplement.Source, Freshness: "historical_values_not_revalidated", AppliedFields: []string{}, RawAPIPaths: []string{}}
	}
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
	if err := x.document("user/last_activities", before); err != nil {
		return err
	}
	for _, resource := range []string{"user/settings", "user/profile", "user/stats"} {
		x.wait()
		data, err := x.source.GetBackupDocument(x.options.User, resource)
		if err != nil {
			return fmt.Errorf("%s: %w", resource, err)
		}
		if x.options.WebsiteSupplement != nil && (resource == "user/settings" || resource == "user/profile") {
			merged, applied, err := x.options.WebsiteSupplement.merge(resource, data)
			if err != nil {
				return fmt.Errorf("%s: %w", resource, err)
			}
			rawPath := "raw-api/" + resource
			if err := x.document(rawPath, data); err != nil {
				return err
			}
			x.manifest.Supplement.RawAPIPaths = append(x.manifest.Supplement.RawAPIPaths, rawPath+".json")
			x.manifest.Supplement.AppliedFields = append(x.manifest.Supplement.AppliedFields, applied...)
			data = merged
		}
		if err := x.document(resource, data); err != nil {
			return err
		}
	}
	for _, resource := range pageResources {
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

var pageResources = []string{
	"history", "watchlist", "ratings/movies", "ratings/shows", "ratings/seasons", "ratings/episodes", "collection/movies", "collection/shows", "collection/episodes", "favorites",
	"playback", "watched/movies", "watched/shows", "hidden/calendar", "hidden/progress_watched", "hidden/progress_watched_reset", "hidden/progress_collected", "hidden/recommendations",
	"comments/movies", "comments/shows", "comments/seasons", "comments/episodes", "comments/lists",
	"notes/movies", "notes/shows", "notes/seasons", "notes/episodes", "notes/people", "notes/activities", "notes/collection_items", "notes/ratings",
	"network/requests", "network/followers", "network/following", "network/friends", "likes/comments", "likes/lists", "saved_filters", "collaborations",
}

func (x *exporter) document(name string, raw json.RawMessage) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || len(object) == 0 {
		return fmt.Errorf("%s: snapshot must be a nonempty object", name)
	}
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := name + ".json"
	w, err := x.zip.Create(path)
	if err != nil {
		return err
	}
	if _, err = w.Write(data); err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	x.manifest.Files = append(x.manifest.Files, File{Path: path, Records: 1, SHA256: hex.EncodeToString(digest[:])})
	x.manifest.Resources = append(x.manifest.Resources, Resource{Name: name, Expected: 1, Records: 1, Pages: 1, Complete: true})
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
			id, err := recordIdentity(resource, raw)
			if err != nil || seen[id] {
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

// History and list rows have entry IDs; ratings and collections identify media.
// In particular, episode/season IDs must not be replaced by their parent show ID.
func recordIdentity(resource string, raw json.RawMessage) (string, error) {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		return "", err
	}
	identity := record["id"]
	if resource == "lists" || resource == "collaborations" {
		var ids struct {
			Trakt json.Number `json:"trakt"`
		}
		if err := json.Unmarshal(record["ids"], &ids); err != nil {
			return "", err
		}
		identity = json.RawMessage(ids.Trakt.String())
	} else if strings.HasPrefix(resource, "ratings/") || strings.HasPrefix(resource, "collection/") || strings.HasPrefix(resource, "watched/") {
		mediaType := strings.TrimSuffix(strings.SplitN(resource, "/", 2)[1], "s")
		var media struct {
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		}
		if err := json.Unmarshal(record[mediaType], &media); err != nil {
			return "", err
		}
		identity = json.RawMessage(media.IDs.Trakt.String())
	} else if strings.HasPrefix(resource, "hidden/") {
		var kind string
		if err := json.Unmarshal(record["type"], &kind); err != nil {
			return "", err
		}
		if kind != "movie" && kind != "show" && kind != "season" {
			return "", fmt.Errorf("unsupported hidden item type")
		}
		var media struct {
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		}
		if err := json.Unmarshal(record[kind], &media); err != nil {
			return "", err
		}
		id, err := positiveIdentity(json.RawMessage(media.IDs.Trakt.String()))
		if err != nil {
			return "", err
		}
		return kind + ":" + id, nil
	} else if strings.HasPrefix(resource, "comments/") || resource == "likes/comments" {
		var comment struct {
			ID json.Number `json:"id"`
		}
		if err := json.Unmarshal(record["comment"], &comment); err != nil {
			return "", err
		}
		identity = json.RawMessage(comment.ID.String())
	} else if resource == "likes/lists" {
		var list struct {
			ID  json.Number `json:"id"`
			IDs struct {
				Trakt json.Number `json:"trakt"`
			} `json:"ids"`
		}
		if err := json.Unmarshal(record["list"], &list); err != nil {
			return "", err
		}
		id := list.IDs.Trakt.String()
		if id == "" {
			id = list.ID.String()
		}
		identity = json.RawMessage(id)
	} else if strings.HasPrefix(resource, "network/") && resource != "network/requests" {
		var user struct {
			IDs struct {
				Slug string `json:"slug"`
			} `json:"ids"`
		}
		if err := json.Unmarshal(record["user"], &user); err != nil {
			return "", err
		}
		if strings.TrimSpace(user.IDs.Slug) == "" {
			return "", fmt.Errorf("missing user slug")
		}
		return user.IDs.Slug, nil
	}
	return positiveIdentity(identity)
}

func positiveIdentity(identity json.RawMessage) (string, error) {
	var id json.Number
	if err := json.Unmarshal(identity, &id); err != nil {
		return "", err
	}
	n, err := strconv.ParseInt(id.String(), 10, 64)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("invalid positive record ID")
	}
	return strconv.FormatInt(n, 10), nil
}
