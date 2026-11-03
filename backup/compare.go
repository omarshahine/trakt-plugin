package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const maxCompareEntry = 16 << 20
const maxCompareArchive = 512 << 20

type CompareInput struct {
	Format       string                `json:"format"`
	Integrity    string                `json:"integrity"`
	Completeness string                `json:"completeness"`
	Mode         string                `json:"mode,omitempty"`
	DataFiles    int                   `json:"data_files"`
	Resources    int                   `json:"resources"`
	Supplement   *SupplementProvenance `json:"supplement,omitempty"`
}

type ChangedSample struct {
	Kind   string   `json:"kind"`
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
}

type ResourceComparison struct {
	MetadataOnlyChanged int             `json:"metadata_only_changed"`
	RecordDataChanged   int             `json:"record_data_changed"`
	SnapshotChanged     int             `json:"snapshot_changed"`
	Name                string          `json:"name"`
	Coverage            string          `json:"coverage"`
	Before              int             `json:"before"`
	After               int             `json:"after"`
	Added               *int            `json:"added"`
	Removed             *int            `json:"removed"`
	Changed             *int            `json:"changed"`
	AddedSample         []string        `json:"added_sample,omitempty"`
	RemovedSample       []string        `json:"removed_sample,omitempty"`
	ChangedSample       []ChangedSample `json:"changed_sample,omitempty"`
}

type Comparison struct {
	MetadataOnlyChanged int                  `json:"metadata_only_changed"`
	RecordDataChanged   int                  `json:"record_data_changed"`
	SnapshotChanged     int                  `json:"snapshot_changed"`
	AccountMatch        string               `json:"account_match"`
	Before              CompareInput         `json:"before"`
	After               CompareInput         `json:"after"`
	Resources           []ResourceComparison `json:"resources"`
	Added               int                  `json:"added"`
	Removed             int                  `json:"removed"`
	Changed             int                  `json:"changed"`
	Warnings            []string             `json:"warnings"`
}

type comparisonArchive struct {
	info CompareInput
	data map[string]map[string]json.RawMessage
}

// Compare opens local ZIPs only. It neither loads credentials nor calls Trakt.
// Samples contain IDs and field paths, never before/after field values.
func Compare(before, after string, sampleLimit int) (Comparison, error) {
	var report Comparison
	if sampleLimit < 0 || sampleLimit > 100 {
		return report, fmt.Errorf("sample-limit must be 0-100")
	}
	a, err := readComparisonArchive(before)
	if err != nil {
		return report, fmt.Errorf("before archive: %w", err)
	}
	b, err := readComparisonArchive(after)
	if err != nil {
		return report, fmt.Errorf("after archive: %w", err)
	}
	report = Comparison{Before: a.info, After: b.info, Resources: []ResourceComparison{}, Warnings: []string{}}
	accountA, err := comparisonAccount(a)
	if err != nil {
		return report, err
	}
	accountB, err := comparisonAccount(b)
	if err != nil {
		return report, err
	}
	matched, err := matchComparisonAccount(accountA, accountB)
	if err != nil {
		return report, err
	}
	report.AccountMatch = "unverified"
	if matched {
		report.AccountMatch = "matched"
	} else {
		report.Warnings = append(report.Warnings, "Account identity could not be matched from both archives; record differences may involve different accounts.")
	}
	if a.info.Completeness == "unknown" || b.info.Completeness == "unknown" {
		report.Warnings = append(report.Warnings, "Website-layout ZIPs have no exporter completeness manifest; CRC, JSON and identity checks do not prove full account coverage.")
	}
	if a.info.Supplement != nil || b.info.Supplement != nil {
		report.Warnings = append(report.Warnings, "Supplemented values are historical and not revalidated; review freshness even when no differences are reported.")
		if a.info.Supplement != nil && b.info.Supplement != nil && a.info.Supplement.Source.SHA256 == b.info.Supplement.Source.SHA256 {
			report.Warnings = append(report.Warnings, "Both backups use the same saved website supplement; unchanged values do not prove they are current.")
		}
	}
	names := map[string]bool{}
	for name := range a.data {
		names[name] = true
	}
	for name := range b.data {
		names[name] = true
	}
	keys := sortedKeys(names)
	for _, name := range keys {
		old, oldOK := a.data[name]
		newRows, newOK := b.data[name]
		r := ResourceComparison{Name: name, Before: len(old), After: len(newRows)}
		if !oldOK {
			r.Coverage = "only_in_after"
		} else if !newOK {
			r.Coverage = "only_in_before"
		} else {
			r.Coverage = "both"
			added, removed, changed := 0, 0, 0
			for _, id := range sortedKeys(newRows) {
				newRow := newRows[id]
				oldRow, exists := old[id]
				if !exists {
					added++
					if len(r.AddedSample) < sampleLimit {
						r.AddedSample = append(r.AddedSample, id)
					}
					continue
				}
				if !bytes.Equal(oldRow, newRow) {
					changed++
					fields := changedFields(oldRow, newRow)
					kind := changeKind(name, fields)
					switch kind {
					case "media_metadata_only":
						r.MetadataOnlyChanged++
					case "snapshot":
						r.SnapshotChanged++
					default:
						r.RecordDataChanged++
					}
					if len(r.ChangedSample) < sampleLimit {
						if len(fields) > 20 {
							fields = append(fields[:20], "[additional field paths omitted]")
						}
						r.ChangedSample = append(r.ChangedSample, ChangedSample{ID: id, Fields: fields, Kind: kind})
					}
				}
			}
			for _, id := range sortedKeys(old) {
				if _, exists := newRows[id]; !exists {
					removed++
					if len(r.RemovedSample) < sampleLimit {
						r.RemovedSample = append(r.RemovedSample, id)
					}
				}
			}
			r.Added = &added
			r.Removed = &removed
			r.Changed = &changed
			report.Added += added
			report.Removed += removed
			report.Changed += changed
			report.MetadataOnlyChanged += r.MetadataOnlyChanged
			report.RecordDataChanged += r.RecordDataChanged
			report.SnapshotChanged += r.SnapshotChanged
		}
		report.Resources = append(report.Resources, r)
	}
	report.Warnings = append(report.Warnings, "Counts reflect archive differences, not necessarily account actions; metadata, API availability and snapshot times can differ. Uncovered resources are not counted as added or removed.")
	return report, nil
}

func changeKind(resource string, fields []string) string {
	if snapshotResource(resource) {
		return "snapshot"
	}
	for _, field := range fields {
		media := false
		for _, prefix := range []string{"movie", "show", "season", "episode", "person"} {
			if field == prefix || strings.HasPrefix(field, prefix+".") {
				media = true
				break
			}
		}
		if !media {
			return "record_data"
		}
	}
	return "media_metadata_only"
}

type archiveAccount struct{ slug, uuid, numeric string }

func matchComparisonAccount(a, b archiveAccount) (bool, error) {
	matched := false
	for _, pair := range [][2]string{{a.uuid, b.uuid}, {a.numeric, b.numeric}} {
		if pair[0] != "" && pair[1] != "" {
			if pair[0] != pair[1] {
				return false, fmt.Errorf("archive account identities conflict")
			}
			matched = true
		}
	}
	if !matched && a.slug != "" && b.slug != "" {
		if a.slug != b.slug {
			return false, fmt.Errorf("archive account slugs conflict")
		}
		matched = true
	}
	return matched, nil
}

func comparisonAccount(a *comparisonArchive) (archiveAccount, error) {
	identity := archiveAccount{}
	for _, resource := range []string{"user/settings", "user/profile"} {
		raw, ok := a.data[resource]["snapshot"]
		if !ok {
			continue
		}
		object, err := decodeObject(raw)
		if err != nil {
			return identity, err
		}
		if resource == "user/settings" {
			object, err = decodeObject(object["user"])
			if err != nil {
				continue
			}
		}
		ids, err := decodeObject(object["ids"])
		if err != nil {
			continue
		}
		current := archiveAccount{}
		_ = json.Unmarshal(ids["slug"], &current.slug)
		_ = json.Unmarshal(ids["uuid"], &current.uuid)
		if value, err := positiveIdentity(ids["trakt"]); err == nil {
			current.numeric = value
		}
		if _, err := matchComparisonAccount(identity, current); err != nil {
			return identity, fmt.Errorf("inconsistent snapshots within an archive")
		}
		if current.slug != "" {
			identity.slug = current.slug
		}
		if current.uuid != "" {
			identity.uuid = current.uuid
		}
		if current.numeric != "" {
			identity.numeric = current.numeric
		}
	}
	return identity, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func canonicalJSON(raw []byte) (json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return json.Marshal(value)
}

func changedFields(before, after json.RawMessage) []string {
	decode := func(raw json.RawMessage) any {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		_ = d.Decode(&v)
		return v
	}
	out := []string{}
	var visit func(any, any, string)
	visit = func(a, b any, prefix string) {
		am, aok := a.(map[string]any)
		bm, bok := b.(map[string]any)
		if aok && bok {
			names := map[string]bool{}
			for k := range am {
				names[k] = true
			}
			for k := range bm {
				names[k] = true
			}
			for _, k := range sortedKeys(names) {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				av, ae := am[k]
				bv, be := bm[k]
				if !ae || !be {
					out = append(out, p)
				} else {
					visit(av, bv, p)
				}
			}
			return
		}
		aa, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if !bytes.Equal(aa, bb) {
			if prefix == "" {
				prefix = "$"
			}
			out = append(out, prefix)
		}
	}
	visit(decode(before), decode(after), "")
	sort.Strings(out)
	return out
}

func snapshotResource(resource string) bool {
	return resource == "user/settings" || resource == "user/profile" || resource == "user/stats" || resource == "user/last_activities" || resource == "raw-api/user/settings" || resource == "raw-api/user/profile"
}
func knownComparisonResource(resource string) bool {
	if snapshotResource(resource) || resource == "lists" {
		return true
	}
	if strings.HasPrefix(resource, "lists/") {
		n, err := strconv.ParseInt(strings.TrimPrefix(resource, "lists/"), 10, 64)
		return err == nil && n > 0
	}
	for _, name := range pageResources {
		if resource == name {
			return true
		}
	}
	return false
}

func addComparisonData(a *comparisonArchive, resource string, data []byte) (int, error) {
	if !knownComparisonResource(resource) {
		return 0, fmt.Errorf("unsupported resource %s", resource)
	}
	if snapshotResource(resource) {
		object, err := decodeObject(data)
		if err != nil || len(object) == 0 {
			return 0, fmt.Errorf("invalid snapshot %s", resource)
		}
		canonical, err := canonicalJSON(data)
		if err != nil {
			return 0, err
		}
		if _, exists := a.data[resource]; exists {
			return 0, fmt.Errorf("duplicate snapshot %s", resource)
		}
		a.data[resource] = map[string]json.RawMessage{"snapshot": canonical}
		return 1, nil
	}
	var rows []json.RawMessage
	if json.Unmarshal(data, &rows) != nil || rows == nil {
		return 0, fmt.Errorf("resource %s is not a JSON array", resource)
	}
	if a.data[resource] == nil {
		a.data[resource] = map[string]json.RawMessage{}
	}
	idResource := resource
	if strings.HasPrefix(resource, "lists/") {
		idResource = "list_items"
	}
	for _, row := range rows {
		id, err := recordIdentity(idResource, row)
		if err != nil {
			return 0, fmt.Errorf("invalid record identity in %s", resource)
		}
		if _, exists := a.data[resource][id]; exists {
			return 0, fmt.Errorf("duplicate record ID in %s", resource)
		}
		canonical, err := canonicalJSON(row)
		if err != nil {
			return 0, err
		}
		a.data[resource][id] = canonical
	}
	return len(rows), nil
}

var comparisonPageName = regexp.MustCompile(`^page-([0-9]+)\.json$`)
var websitePageSuffix = regexp.MustCompile(`-[0-9]+\.json$`)
var websiteListFile = regexp.MustCompile(`^lists-list-([0-9]+)-.+\.json$`)

func nativeFileResource(name string) (string, int, error) {
	resource := strings.TrimSuffix(name, ".json")
	if snapshotResource(resource) {
		return resource, 1, nil
	}
	match := comparisonPageName.FindStringSubmatch(path.Base(name))
	if match == nil {
		return "", 0, fmt.Errorf("unexpected native file name")
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n < 1 || fmt.Sprintf("page-%06d.json", n) != path.Base(name) {
		return "", 0, fmt.Errorf("invalid page name")
	}
	resource = path.Dir(name)
	if !knownComparisonResource(resource) {
		return "", 0, fmt.Errorf("unsupported native resource")
	}
	return resource, n, nil
}

func websiteFileResource(name string) (string, error) {
	if m := websiteListFile.FindStringSubmatch(name); m != nil {
		return "lists/" + m[1], nil
	}
	stem := strings.TrimSuffix(websitePageSuffix.ReplaceAllString(name, ".json"), ".json")
	explicit := map[string]string{"user-settings": "user/settings", "user-profile": "user/profile", "user-stats": "user/stats", "user-last-activities": "user/last_activities", "user-saved-filters": "saved_filters", "watched-history": "history", "watched-playback": "playback", "lists-watchlist": "watchlist", "lists-favorites": "favorites", "lists-lists": "lists", "lists-collaborations": "collaborations", "network-followers-requests": "network/requests"}
	if resource, ok := explicit[stem]; ok {
		return resource, nil
	}
	for _, prefix := range []string{"hidden", "network", "likes", "comments", "notes", "ratings", "watched", "collection"} {
		if strings.HasPrefix(stem, prefix+"-") {
			resource := prefix + "/" + strings.ReplaceAll(strings.TrimPrefix(stem, prefix+"-"), "-", "_")
			if knownComparisonResource(resource) {
				return resource, nil
			}
		}
	}
	return "", fmt.Errorf("unrecognized website-layout JSON file")
}

func readComparisonArchive(filename string) (*comparisonArchive, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open ZIP")
	}
	defer z.Close()
	if len(z.File) > 10000 {
		return nil, fmt.Errorf("too many ZIP members")
	}
	entries := map[string]*zip.File{}
	var total uint64
	for _, entry := range z.File {
		name := entry.Name
		if path.IsAbs(name) || strings.Contains(name, "\\") || path.Clean(strings.TrimSuffix(name, "/")) != strings.TrimSuffix(name, "/") || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("unsafe ZIP member name")
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if _, exists := entries[name]; exists {
			return nil, fmt.Errorf("duplicate ZIP member")
		}
		if entry.UncompressedSize64 > maxCompareEntry {
			return nil, fmt.Errorf("ZIP member exceeds size limit")
		}
		total += entry.UncompressedSize64
		if total > maxCompareArchive {
			return nil, fmt.Errorf("ZIP exceeds expanded size limit")
		}
		entries[name] = entry
	}
	read := func(name string) ([]byte, error) {
		entry := entries[name]
		if entry == nil {
			return nil, fmt.Errorf("missing ZIP member")
		}
		r, err := entry.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, maxCompareEntry+1))
		if err != nil || len(data) > maxCompareEntry {
			return nil, fmt.Errorf("ZIP member failed CRC/read/size validation")
		}
		return data, nil
	}
	a := &comparisonArchive{data: map[string]map[string]json.RawMessage{}}
	if _, native := entries["manifest.json"]; native {
		data, err := read("manifest.json")
		if err != nil {
			return nil, err
		}
		var m Manifest
		if json.Unmarshal(data, &m) != nil || m.Version != 1 || !m.Complete || m.Consistency != "activity_marker_unchanged" || len(m.Scopes) == 0 || len(m.Errors) > 0 || len(m.Files) == 0 || len(m.Resources) == 0 {
			return nil, fmt.Errorf("invalid, unsupported or incomplete native manifest")
		}
		if len(entries) != len(m.Files)+1 {
			return nil, fmt.Errorf("native ZIP has unlisted/missing files")
		}
		seen := map[string]bool{}
		pages := map[string]map[int]bool{}
		for _, entry := range m.Files {
			if seen[entry.Path] || entry.Path == "manifest.json" {
				return nil, fmt.Errorf("duplicate/invalid manifest file")
			}
			seen[entry.Path] = true
			data, err := read(entry.Path)
			if err != nil {
				return nil, err
			}
			digest := sha256.Sum256(data)
			if entry.SHA256 != hex.EncodeToString(digest[:]) {
				return nil, fmt.Errorf("file SHA-256 mismatch")
			}
			resource, page, err := nativeFileResource(entry.Path)
			if err != nil {
				return nil, err
			}
			count, err := addComparisonData(a, resource, data)
			if err != nil {
				return nil, err
			}
			if count != entry.Records {
				return nil, fmt.Errorf("file record-count mismatch")
			}
			if pages[resource] == nil {
				pages[resource] = map[int]bool{}
			}
			pages[resource][page] = true
		}
		seenResources := map[string]bool{}
		for _, r := range m.Resources {
			resource := r.Name
			if strings.HasPrefix(resource, "list:") {
				resource = "lists/" + strings.TrimPrefix(resource, "list:")
			}
			if seenResources[resource] || !r.Complete || r.Records < 0 || r.Expected != r.Records || r.Pages < 1 || len(a.data[resource]) != r.Records || len(pages[resource]) != r.Pages {
				return nil, fmt.Errorf("manifest resource count/completeness mismatch")
			}
			seenResources[resource] = true
			for page := 1; page <= r.Pages; page++ {
				if !pages[resource][page] {
					return nil, fmt.Errorf("missing native page")
				}
			}
		}
		if len(seenResources) != len(a.data) {
			return nil, fmt.Errorf("unaccounted resource")
		}
		a.info = CompareInput{Format: "native_manifest", Integrity: "manifest_sha256_counts_ids", Completeness: "declared_scope_complete", Mode: m.Mode, DataFiles: len(m.Files), Supplement: m.Supplement}
	} else {
		if len(entries) == 0 {
			return nil, fmt.Errorf("empty ZIP")
		}
		websitePages := map[string]map[int]bool{}
		for _, name := range sortedKeys(entries) {
			if !strings.HasSuffix(name, ".json") {
				return nil, fmt.Errorf("unsupported website-layout ZIP member")
			}
			resource, err := websiteFileResource(name)
			if err != nil {
				return nil, err
			}
			data, err := read(name)
			if err != nil {
				return nil, err
			}
			if _, err := addComparisonData(a, resource, data); err != nil {
				return nil, err
			}
			if websitePageSuffix.MatchString(name) && !strings.HasPrefix(name, "lists-list-") {
				base := strings.TrimSuffix(name, ".json")
				n, _ := strconv.Atoi(base[strings.LastIndex(base, "-")+1:])
				if n < 1 {
					return nil, fmt.Errorf("invalid website page number")
				}
				if websitePages[resource] == nil {
					websitePages[resource] = map[int]bool{}
				}
				if websitePages[resource][n] {
					return nil, fmt.Errorf("duplicate website page")
				}
				websitePages[resource][n] = true
			}
		}
		for _, pages := range websitePages {
			for n := 1; n <= len(pages); n++ {
				if !pages[n] {
					return nil, fmt.Errorf("missing intermediate website page")
				}
			}
		}
		a.info = CompareInput{Format: "website_export_layout", Integrity: "zip_crc_json_ids_only", Completeness: "unknown", DataFiles: len(entries)}
	}
	// Raw snapshots are validated above but are not counted as business-data changes.
	delete(a.data, "raw-api/user/settings")
	delete(a.data, "raw-api/user/profile")
	a.info.Resources = len(a.data)
	return a, nil
}
