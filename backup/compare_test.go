package backup

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeComparisonFixture(t *testing.T, f *fakeSource) string {
	t.Helper()
	o := options(t)
	if _, err := Export(f, o); err != nil {
		t.Fatal(err)
	}
	return o.Output
}

func changeRow(t *testing.T, row json.RawMessage, field, value string) json.RawMessage {
	t.Helper()
	object, err := decodeObject(row)
	if err != nil {
		t.Fatal(err)
	}
	object[field] = raw(value)
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func comparisonResource(t *testing.T, c Comparison, name string) ResourceComparison {
	t.Helper()
	for _, r := range c.Resources {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("missing resource %s", name)
	return ResourceComparison{}
}

func TestCompareChangesAndValueFreeSamples(t *testing.T) {
	before := nativeComparisonFixture(t, fixture())
	f := fixture()
	f.data["history"] = append(f.data["history"][1:], raw(`{"id":14,"watched_at":"2026-02-01T00:00:00Z"}`))
	f.data["ratings/movies"][0] = changeRow(t, f.data["ratings/movies"][0], "rating", "9")
	f.data["list_items:7"][0] = changeRow(t, f.data["list_items:7"][0], "notes", `"private new note never print"`)
	f.data["watched/shows"][0] = changeRow(t, f.data["watched/shows"][0], "show", `{"ids":{"trakt":55},"aired_episodes":50}`)
	after := nativeComparisonFixture(t, f)
	c, err := Compare(before, after, 2)
	if err != nil {
		t.Fatal(err)
	}
	h := comparisonResource(t, c, "history")
	if *h.Added != 1 || *h.Removed != 1 || *h.Changed != 0 || h.AddedSample[0] != "14" || h.RemovedSample[0] != "11" {
		t.Fatalf("history diff: %+v", h)
	}
	r := comparisonResource(t, c, "ratings/movies")
	if *r.Changed != 1 || r.ChangedSample[0].Fields[0] != "rating" {
		t.Fatalf("rating diff: %+v", r)
	}
	l := comparisonResource(t, c, "lists/7")
	if *l.Changed != 1 || l.ChangedSample[0].Fields[0] != "notes" {
		t.Fatalf("list diff: %+v", l)
	}
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), "private new note never print") || strings.Contains(string(data), "synthetic reply") {
		t.Fatal("comparison leaked field values")
	}
	if c.Before.Integrity != "manifest_sha256_counts_ids" || c.Added != 1 || c.Removed != 1 || c.Changed != 3 || c.MetadataOnlyChanged != 1 || c.RecordDataChanged != 2 {
		t.Fatalf("bad summary: %+v", c)
	}
	again, err := Compare(before, after, 2)
	if err != nil {
		t.Fatal(err)
	}
	data2, _ := json.Marshal(again)
	if string(data) != string(data2) {
		t.Fatal("nondeterministic comparison")
	}
	zero, err := Compare(before, after, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range zero.Resources {
		if len(r.AddedSample)+len(r.RemovedSample)+len(r.ChangedSample) != 0 {
			t.Fatal("sample limit ignored")
		}
	}
}

func rewriteComparisonZIP(t *testing.T, input string, edit func(map[string][]byte)) string {
	t.Helper()
	files := archive(t, input)
	edit(files)
	output := filepath.Join(t.TempDir(), "modified.zip")
	f, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range sortedKeys(files) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return output
}

func TestCompareRejectsInvalidNativeArchives(t *testing.T) {
	valid := nativeComparisonFixture(t, fixture())
	for _, scenario := range []string{"checksum", "unlisted", "missing", "partial", "file_count", "resource_count", "missing_page", "invalid_version", "unverified_activity"} {
		t.Run(scenario, func(t *testing.T) {
			broken := rewriteComparisonZIP(t, valid, func(files map[string][]byte) {
				var m Manifest
				if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "checksum":
					files["history/page-000001.json"] = []byte(`[]`)
				case "unlisted":
					files["extra.json"] = []byte(`[]`)
				case "missing":
					delete(files, "history/page-000001.json")
				case "partial":
					m.Complete = false
				case "file_count":
					m.Files[0].Records++
				case "resource_count":
					m.Resources[0].Expected++
				case "invalid_version":
					m.Version = 99
				case "unverified_activity":
					m.Consistency = "unverified"
				case "missing_page":
					for i, e := range m.Files {
						if e.Path == "history/page-000002.json" {
							files["history/page-000003.json"] = files[e.Path]
							delete(files, e.Path)
							m.Files[i].Path = "history/page-000003.json"
						}
					}
				}
				files["manifest.json"], _ = json.Marshal(m)
			})
			if _, err := Compare(valid, broken, 1); err == nil {
				t.Fatal("unsafe native archive accepted")
			}
		})
	}
}

func TestCompareCoverageDifferenceIsNotDeletion(t *testing.T) {
	before := nativeComparisonFixture(t, fixture())
	after := rewriteComparisonZIP(t, before, func(files map[string][]byte) {
		var m Manifest
		if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
			t.Fatal(err)
		}
		kept := []File{}
		for _, e := range m.Files {
			if strings.HasPrefix(e.Path, "ratings/movies/") {
				delete(files, e.Path)
			} else {
				kept = append(kept, e)
			}
		}
		m.Files = kept
		resources := []Resource{}
		for _, r := range m.Resources {
			if r.Name != "ratings/movies" {
				resources = append(resources, r)
			}
		}
		m.Resources = resources
		files["manifest.json"], _ = json.Marshal(m)
	})
	c, err := Compare(before, after, 3)
	if err != nil {
		t.Fatal(err)
	}
	r := comparisonResource(t, c, "ratings/movies")
	if r.Coverage != "only_in_before" || r.Removed != nil || c.Removed != 0 {
		t.Fatalf("coverage mistaken for removal: %+v", r)
	}
}

func writeWebsiteComparisonZIP(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "website.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, name := range sortedKeys(files) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWebsiteLayoutCanonicalizationAndLargeIDs(t *testing.T) {
	a := writeWebsiteComparisonZIP(t, map[string]string{"watched-history-1.json": `[{"id":9007199254740993,"watched_at":"2026-01-01","extra":null}]`})
	b := writeWebsiteComparisonZIP(t, map[string]string{"watched-history-1.json": `[ { "extra":null,"watched_at":"2026-01-01","id":9007199254740993} ]`})
	c, err := Compare(a, b, 1)
	if err != nil || c.Changed != 0 {
		t.Fatalf("formatting mistaken for change: %+v %v", c, err)
	}
	if c.Before.Completeness != "unknown" || c.Before.Integrity != "zip_crc_json_ids_only" {
		t.Fatal("unsigned website completeness claimed")
	}
	d := writeWebsiteComparisonZIP(t, map[string]string{"watched-history-1.json": `[{"id":9007199254740993,"watched_at":"2026-01-02","extra":null}]`})
	c, err = Compare(a, d, 1)
	if err != nil || c.Resources[0].ChangedSample[0].ID != "9007199254740993" {
		t.Fatal("lost large ID precision")
	}
}

func TestWebsiteLayoutRejectsInvalidInputs(t *testing.T) {
	for _, files := range []map[string]string{
		{"watched-history-1.json": `[{"id":1},{"id":1}]`},
		{"watched-history-1.json": `[{"id":1}]`, "watched-history-3.json": `[{"id":2}]`},
		{"watched-history-1.json": `null`},
		{"unknown.json": `[]`},
		{"../watched-history-1.json": `[]`},
		{"watched-history-1.json": `[] trailing`},
	} {
		p := writeWebsiteComparisonZIP(t, files)
		if _, err := Compare(p, p, 0); err == nil {
			t.Fatal("invalid website archive accepted")
		}
	}
}

func TestCompareAccountsAndSupplementFreshness(t *testing.T) {
	f := supplementSource()
	o := options(t)
	o.WebsiteSupplement = importedSupplement(t)
	if _, err := Export(f, o); err != nil {
		t.Fatal(err)
	}
	c, err := Compare(o.Output, o.Output, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.AccountMatch != "matched" || c.Changed != 0 || !strings.Contains(strings.Join(c.Warnings, " "), "same saved website supplement") {
		t.Fatal("missing account/freshness evidence")
	}
	for _, r := range c.Resources {
		if strings.HasPrefix(r.Name, "raw-api/") {
			t.Fatal("raw API snapshots counted as extra business data")
		}
	}
	other := writeWebsiteComparisonZIP(t, map[string]string{"user-profile.json": `{"ids":{"slug":"other-user","trakt":99}}`})
	if _, err := Compare(o.Output, other, 0); err == nil {
		t.Fatal("different accounts compared")
	}
}

func TestNoContentStatsRoundTrip(t *testing.T) {
	f := supplementSource()
	f.documentData["user/stats"] = raw(`null`)
	zip := nativeComparisonFixture(t, f)
	c, err := Compare(zip, zip, 0)
	if err != nil || c.Changed != 0 {
		t.Fatalf("no-content stats rejected: %+v %v", c, err)
	}
	files := archive(t, zip)
	var m Manifest
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range m.Files {
		if file.Path == "user/stats.json" {
			found = true
			if file.HTTPStatus != 204 || string(files[file.Path]) != "null\n" {
				t.Fatal("HTTP absence was not recorded")
			}
		}
	}
	if !found {
		t.Fatal("missing stats snapshot")
	}
	bad := rewriteComparisonZIP(t, zip, func(files map[string][]byte) {
		for i := range m.Files {
			m.Files[i].HTTPStatus = 0
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		files["manifest.json"] = data
	})
	if _, err := Compare(bad, bad, 0); err == nil {
		t.Fatal("unmarked null stats accepted")
	}
}
