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
	data         map[string][]json.RawMessage
	requests     []string
	markers      int
	changed      bool
	documentData map[string]json.RawMessage
	intercept    func(string, int, []json.RawMessage, api.Pagination) ([]json.RawMessage, api.Pagination, error)
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }
func fixture() *fakeSource {
	f := &fakeSource{data: map[string][]json.RawMessage{
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
	for _, resource := range pageResources {
		if _, ok := f.data[resource]; ok {
			continue
		}
		var row json.RawMessage
		switch {
		case strings.HasPrefix(resource, "watched/"):
			kind := strings.TrimSuffix(strings.Split(resource, "/")[1], "s")
			row = raw(fmt.Sprintf(`{"%s":{"ids":{"trakt":55}},"plays":2,"reset_at":null}`, kind))
		case strings.HasPrefix(resource, "hidden/"):
			row = raw(`{"hidden_at":"2026-01-01T00:00:00Z","type":"show","show":{"ids":{"trakt":55}}}`)
		case strings.HasPrefix(resource, "comments/") || resource == "likes/comments":
			row = raw(`{"comment":{"id":55,"parent_id":12,"comment":"synthetic reply"}}`)
		case resource == "likes/lists":
			row = raw(`{"list":{"ids":{"trakt":55}},"liked_at":"2026-01-01T00:00:00Z"}`)
		case resource == "collaborations":
			row = raw(`{"ids":{"trakt":55},"name":"Synthetic collaboration","user":{"ids":{"slug":"synthetic-owner"}}}`)
		case strings.HasPrefix(resource, "network/") && resource != "network/requests":
			row = raw(`{"user":{"ids":{"slug":"synthetic-friend"}},"followed_at":"2026-01-01T00:00:00Z"}`)
		default:
			row = raw(`{"id":55,"future_field":true}`)
		}
		f.data[resource] = []json.RawMessage{row}
	}
	// IDs overlap across media types; they are distinct hidden entries.
	f.data["hidden/calendar"] = append(f.data["hidden/calendar"], raw(`{"type":"movie","movie":{"ids":{"trakt":55}}}`))
	return f
}
func (f *fakeSource) GetBackupActivity() (json.RawMessage, error) {
	f.markers++
	if f.changed && f.markers > 1 {
		return raw(`{"all":"changed"}`), nil
	}
	return raw(`{"all":"same"}`), nil
}
func (f *fakeSource) GetBackupDocument(_ string, resource string) (json.RawMessage, error) {
	if f.documentData != nil {
		return f.documentData[resource], nil
	}
	return raw(`{"synthetic":true}`), nil
}

func namedResource(m Manifest, name string) Resource {
	for _, r := range m.Resources {
		if r.Name == name {
			return r
		}
	}
	return Resource{}
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
	if !m.Complete || m.Consistency != "activity_marker_unchanged" || len(m.Resources) != 45 || f.markers != 2 {
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
		if strings.HasPrefix(entry.Path, "user/") {
			var doc map[string]json.RawMessage
			if err := json.Unmarshal(b, &doc); err != nil || len(doc) == 0 || entry.Records != 1 {
				t.Fatalf("bad document %s", entry.Path)
			}
		} else if err := json.Unmarshal(b, &rows); err != nil || len(rows) != entry.Records {
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
	if namedResource(m, "history").Records != 3 {
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
		if path != "manifest.json" && !strings.HasPrefix(path, "user/") && strings.TrimSpace(string(b)) != "[]" {
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
					if err != nil || !m.Complete || namedResource(m, "history").Records != 9 || namedResource(m, "history").Expected != 9 {
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
	for _, resource := range pageResources {
		for _, scenario := range []string{"missing", "duplicate", "denied", "missing_headers"} {
			t.Run(resource+"/"+scenario, func(t *testing.T) {
				f := fixture()
				switch scenario {
				case "missing":
					f.data[resource][0] = raw(`{}`)
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

func TestExportRejectsMalformedSnapshots(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{}`, `invalid`} {
		t.Run(body, func(t *testing.T) {
			f := fixture()
			f.documentData = map[string]json.RawMessage{"user/settings": raw(body)}
			o := options(t)
			m, err := Export(f, o)
			if err == nil || m.Complete {
				t.Fatal("invalid snapshot accepted")
			}
			var stored Manifest
			if err := json.Unmarshal(archive(t, o.Output)["manifest.json"], &stored); err != nil || stored.Complete {
				t.Fatal("incomplete snapshot manifest missing")
			}
		})
	}
}

func TestWebsiteFieldsAndReplyIdentityPreserved(t *testing.T) {
	f := fixture()
	f.documentData = map[string]json.RawMessage{
		"user/settings": raw(`{"browsing":{"language":"en"},"sharing":{"twitter":false}}`),
		"user/profile":  raw(`{"ids":{"slug":"synthetic-user"},"vip":true}`),
		"user/stats":    raw(`{"total_plays":3}`),
	}
	f.data["comments/shows"] = append(f.data["comments/shows"], raw(`{"comment":{"id":56,"parent_id":12,"comment":"second synthetic reply"}}`))
	o := options(t)
	m, err := Export(f, o)
	if err != nil || !m.Complete {
		t.Fatalf("%+v %v", m, err)
	}
	files := archive(t, o.Output)
	for _, check := range []struct{ path, field string }{
		{"user/settings.json", `"sharing"`}, {"user/profile.json", `"vip": true`}, {"user/stats.json", `"total_plays": 3`}, {"user/last_activities.json", `"all": "same"`},
		{"comments/shows/page-000001.json", `second synthetic reply`}, {"watched/shows/page-000001.json", `"reset_at": null`},
	} {
		if !strings.Contains(string(files[check.path]), check.field) {
			t.Fatalf("missing website field %s", check.path)
		}
	}
	if namedResource(m, "comments/shows").Records != 2 {
		t.Fatal("replies deduplicated by parent")
	}
}
