package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/omarshahine/trakt-plugin/api"
)

type fakeSource struct {
	data      map[string][]json.RawMessage
	requests  []string
	markers   int
	changed   bool
	intercept func(string, int, []json.RawMessage, api.Pagination) ([]json.RawMessage, api.Pagination, error)
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }
func fixture() *fakeSource {
	return &fakeSource{data: map[string][]json.RawMessage{
		"history":             {raw(`{"id":11,"watched_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}`), raw(`{"id":12,"watched_at":"2026-01-02T00:00:00Z","movie":{"ids":{"trakt":55}}}`), raw(`{"id":13,"action":"watch","type":"episode","episode":{"ids":{"trakt":56}}}`)},
		"watchlist":           {raw(`{"id":21,"rank":1,"notes":"synthetic note","my_rating":8}`)},
		"lists":               {raw(`{"name":"Synthetic","ids":{"trakt":7},"privacy":"private","sort_by":"rank","item_count":3,"future_field":true}`)},
		"list_items:7":        {raw(`{"id":31,"type":"movie","movie":{"ids":{"trakt":55}}}`), raw(`{"id":32,"type":"person","person":{"ids":{"trakt":66}}}`), raw(`{"id":33,"type":"season","season":{"number":2},"notes":null}`)},
		"ratings/movies":      {raw(`{"rating":8,"rated_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}}}`), raw(`{"rating":7,"movie":{"ids":{"trakt":56}}}`), raw(`{"rating":9,"movie":{"ids":{"trakt":57}}}`)},
		"ratings/shows":       {raw(`{"rating":8,"show":{"ids":{"trakt":55}}}`)},
		"ratings/seasons":     {raw(`{"rating":8,"season":{"ids":{"trakt":55},"number":1},"show":{"ids":{"trakt":99}}}`)},
		"ratings/episodes":    {raw(`{"rating":8,"episode":{"ids":{"trakt":55}},"show":{"ids":{"trakt":99}}}`), raw(`{"rating":9,"episode":{"ids":{"trakt":56}},"show":{"ids":{"trakt":99}}}`)},
		"collection/movies":   {raw(`{"collected_at":"2026-01-01T00:00:00Z","movie":{"ids":{"trakt":55}},"metadata":{"media_type":"digital","resolution":"hd_1080p","audio":"dts"}}`)},
		"collection/shows":    {raw(`{"show":{"ids":{"trakt":55}},"seasons":[{"number":1,"episodes":[{"number":1,"collected_at":"2026-01-01T00:00:00Z","metadata":{"resolution":"hd_1080p"}}]}]}`)},
		"collection/episodes": {raw(`{"episode":{"ids":{"trakt":55}},"show":{"ids":{"trakt":99}},"metadata":{"audio":"dts"}}`)},
		"favorites":           {raw(`{"id":55,"rank":1,"type":"movie","movie":{"ids":{"trakt":55}},"notes":"synthetic favorite"}`), raw(`{"id":56,"rank":2,"type":"show","show":{"ids":{"trakt":55}}}`)},
	}}
}
func (f *fakeSource) GetBackupActivity() (json.RawMessage, error) {
	f.markers++
	if f.changed && f.markers > 1 {
		return raw(`{"all":"changed"}`), nil
	}
	return raw(`{"all":"same"}`), nil
}
func (f *fakeSource) GetBackupPage(user, resource, list string, p api.PaginationsParams) ([]json.RawMessage, api.Pagination, error) {
	if user != "synthetic-user" {
		return nil, api.Pagination{}, fmt.Errorf("wrong user")
	}
	key := resource
	if list != "" {
		key += ":" + list
	}
	f.requests = append(f.requests, fmt.Sprintf("%s:%d", key, p.Page))
	all := f.data[key]
	total := len(all)
	pages := (total + p.Limit - 1) / p.Limit
	if pages == 0 {
		pages = 1
	}
	start := (p.Page - 1) * p.Limit
	if start > total {
		start = total
	}
	end := start + p.Limit
	if end > total {
		end = total
	}
	rows := all[start:end]
	pagination := api.Pagination{Page: strconv.Itoa(p.Page), Limit: strconv.Itoa(p.Limit), PageCount: strconv.Itoa(pages), ItemCount: strconv.Itoa(total)}
	if f.intercept != nil {
		return f.intercept(key, p.Page, rows, pagination)
	}
	return rows, pagination, nil
}
func options(t *testing.T) Options {
	t.Helper()
	return Options{User: "synthetic-user", Output: filepath.Join(t.TempDir(), "backup.zip"), Limit: 2, MaxPages: 100}
}
func archive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	result := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := result[f.Name]; ok {
			t.Fatalf("duplicate path %s", f.Name)
		}
		result[f.Name] = b
	}
	return result
}

func TestExportAllPagesAndChecksums(t *testing.T) {
	f := fixture()
	o := options(t)
	m, err := Export(f, o)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Complete || m.Consistency != "activity_marker_unchanged" || len(m.Resources) != 12 || f.markers != 2 {
		t.Fatalf("manifest: %+v", m)
	}
	files := archive(t, o.Output)
	var stored Manifest
	if err := json.Unmarshal(files["manifest.json"], &stored); err != nil {
		t.Fatal(err)
	}
	if !stored.Complete || len(stored.Excluded) == 0 {
		t.Fatal("missing scope/completeness")
	}
	if len(files) != len(m.Files)+1 {
		t.Fatal("unaccounted files")
	}
	for _, entry := range stored.Files {
		b, ok := files[entry.Path]
		if !ok {
			t.Fatalf("missing %s", entry.Path)
		}
		sum := sha256.Sum256(b)
		if entry.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("bad checksum %s", entry.Path)
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(b, &rows); err != nil || len(rows) != entry.Records {
			t.Fatalf("bad count %s", entry.Path)
		}
	}
	for _, r := range m.Resources {
		if !r.Complete || r.Records != r.Expected {
			t.Fatalf("incomplete resource %+v", r)
		}
	}
	if !strings.Contains(string(files["lists/page-000001.json"]), `"future_field": true`) {
		t.Fatal("lost raw metadata")
	}
	if !strings.Contains(string(files["watchlist/page-000001.json"]), `"notes": "synthetic note"`) {
		t.Fatal("lost notes")
	}
	if !strings.Contains(string(files["lists/7/page-000002.json"]), `"notes": null`) {
		t.Fatal("lost null field")
	}
	if m.Resources[0].Records != 3 {
		t.Fatal("rewatches were deduplicated by title")
	}
	for _, scope := range []string{"ratings", "collections", "favorites"} {
		if !slices.Contains(m.Scopes, scope) || slices.Contains(m.Excluded, scope) {
			t.Fatalf("incorrect scope: %s", scope)
		}
	}
	for _, check := range []struct{ path, field string }{
		{"ratings/movies/page-000001.json", `"rated_at"`}, {"ratings/movies/page-000002.json", `"rating": 9`},
		{"collection/movies/page-000001.json", `"audio": "dts"`}, {"collection/shows/page-000001.json", `"episodes"`},
		{"favorites/page-000001.json", `"notes": "synthetic favorite"`},
	} {
		if !strings.Contains(string(files[check.path]), check.field) {
			t.Fatalf("lost field %s", check.path)
		}
	}
}

func TestExportEmptyResources(t *testing.T) {
	f := &fakeSource{data: map[string][]json.RawMessage{}}
	f.intercept = func(_ string, _ int, rows []json.RawMessage, p api.Pagination) ([]json.RawMessage, api.Pagination, error) {
		p.PageCount = "0"
		return rows, p, nil
	}
	o := options(t)
	m, err := Export(f, o)
	if err != nil || !m.Complete {
		t.Fatalf("%+v %v", m, err)
	}
	for path, b := range archive(t, o.Output) {
		if path != "manifest.json" && strings.TrimSpace(string(b)) != "[]" {
			t.Fatalf("empty output %s", b)
		}
	}
}

func TestExportFailuresRetainExplicitPartialManifest(t *testing.T) {
	for _, scenario := range []string{"missing_headers", "short_page", "changing_total", "duplicate_ids", "rate_limit", "denied", "metadata_mismatch", "changed_activity", "max_pages"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture()
			o := options(t)
			if scenario == "changed_activity" {
				f.changed = true
			}
			if scenario == "max_pages" {
				o.MaxPages = 1
			}
			f.intercept = func(key string, page int, rows []json.RawMessage, p api.Pagination) ([]json.RawMessage, api.Pagination, error) {
				if key == "history" {
					switch scenario {
					case "missing_headers":
						p.PageCount = ""
					case "short_page":
						if page == 1 {
							rows = rows[:1]
						}
					case "changing_total":
						if page == 2 {
							p.ItemCount = "4"
						}
					case "duplicate_ids":
						if page == 2 {
							rows = []json.RawMessage{f.data["history"][0]}
						}
					case "rate_limit":
						return nil, p, &api.RateLimitError{Path: "/synthetic", RetryAfterSeconds: 17, Status: "429"}
					case "denied":
						return nil, p, fmt.Errorf("403 Forbidden")
					}
				}
				if key == "list_items:7" && scenario == "metadata_mismatch" {
					p.ItemCount = "4"
				}
				return rows, p, nil
			}
			m, err := Export(f, o)
			if err == nil || m.Complete || len(m.Errors) == 0 {
				t.Fatalf("failure not reported: %+v %v", m, err)
			}
			files := archive(t, o.Output)
			var stored Manifest
			if err := json.Unmarshal(files["manifest.json"], &stored); err != nil {
				t.Fatal(err)
			}
			if stored.Complete || len(stored.Errors) == 0 {
				t.Fatal("partial ZIP claimed complete")
			}
			if scenario == "rate_limit" && (len(f.requests) != 1 || f.markers != 1) {
				t.Fatal("retried after rate limit")
			}
		})
	}
}

func TestExportNeverOverwrites(t *testing.T) {
	f := fixture()
	o := options(t)
	if err := os.WriteFile(o.Output, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(f, o); err == nil {
		t.Fatal("overwrote file")
	}
	b, _ := os.ReadFile(o.Output)
	if string(b) != "existing" || len(f.requests) != 0 || f.markers != 0 {
		t.Fatal("existing file changed or source read")
	}
}

func TestExportRejectsIncorrectTerminalCount(t *testing.T) {
	f := fixture()
	for i := 14; i < 40; i++ {
		f.data["history"] = append(f.data["history"], raw(fmt.Sprintf(`{"id":%d}`, i)))
	}
	f.intercept = func(key string, page int, rows []json.RawMessage, p api.Pagination) ([]json.RawMessage, api.Pagination, error) {
		if key == "history" && page > 1 {
			p.ItemCount = "30"
		}
		return rows, p, nil
	}
	m, err := Export(f, options(t))
	if err == nil || m.Complete || !strings.Contains(err.Error(), "terminal total") {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestExportHistoryReconcilesStaleCounts(t *testing.T) {
	for _, earlier := range []string{"6", "10"} {
		for _, scenario := range []string{"stable", "duplicate", "empty_terminal", "changed_activity", "changed_pages", "changed_limit"} {
			t.Run(earlier+"/"+scenario, func(t *testing.T) {
				f := fixture()
				f.data["history"] = nil
				for i := 1; i <= 9; i++ {
					f.data["history"] = append(f.data["history"], raw(fmt.Sprintf(`{"id":%d}`, i)))
				}
				f.changed = scenario == "changed_activity"
				f.intercept = func(key string, page int, rows []json.RawMessage, p api.Pagination) ([]json.RawMessage, api.Pagination, error) {
					if key == "history" {
						if page == 1 {
							p.ItemCount = earlier
						}
						if page == 2 {
							switch scenario {
							case "duplicate":
								rows = append([]json.RawMessage(nil), rows...)
								rows[0] = f.data["history"][0]
							case "empty_terminal":
								rows = nil
							case "changed_pages":
								p.PageCount = "3"
								p.ItemCount = "11"
							case "changed_limit":
								p.Limit = "6"
							}
						}
					}
					return rows, p, nil
				}
				o := options(t)
				o.Limit = 5
				m, err := Export(f, o)
				if scenario == "stable" {
					if err != nil || !m.Complete || m.Resources[0].Records != 9 || m.Resources[0].Expected != 9 {
						t.Fatalf("stale count not reconciled: %+v %v", m, err)
					}
				} else if err == nil || m.Complete {
					t.Fatalf("unsafe export accepted: %+v %v", m, err)
				}
			})
		}
	}
}

func TestExpandedResourcesRejectInvalidIdentities(t *testing.T) {
	for _, resource := range []string{"ratings/movies", "ratings/shows", "ratings/seasons", "ratings/episodes", "collection/movies", "collection/shows", "collection/episodes", "favorites"} {
		for _, scenario := range []string{"missing", "duplicate", "denied", "missing_headers"} {
			t.Run(resource+"/"+scenario, func(t *testing.T) {
				f := fixture()
				switch scenario {
				case "missing":
					f.data[resource][0] = raw(`{"id":999,"show":{"ids":{"trakt":99}}}`)
					// A parent show or top-level fallback ID is not a media identity.
					if resource == "favorites" || strings.HasSuffix(resource, "/shows") {
						f.data[resource][0] = raw(`{}`)
					}
				case "duplicate":
					f.data[resource] = append(f.data[resource], f.data[resource][0])
				}
				f.intercept = func(key string, _ int, rows []json.RawMessage, p api.Pagination) ([]json.RawMessage, api.Pagination, error) {
					if key == resource {
						if scenario == "denied" {
							return nil, p, fmt.Errorf("403 Forbidden")
						}
						if scenario == "missing_headers" {
							p.ItemCount = ""
						}
					}
					return rows, p, nil
				}
				o := options(t)
				m, err := Export(f, o)
				if err == nil || m.Complete || len(m.Errors) == 0 {
					t.Fatalf("invalid resource accepted: %+v %v", m, err)
				}
				var stored Manifest
				if err := json.Unmarshal(archive(t, o.Output)["manifest.json"], &stored); err != nil || stored.Complete {
					t.Fatal("partial archive not marked incomplete")
				}
			})
		}
	}
}
