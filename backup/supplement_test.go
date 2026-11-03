package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func websiteFixture(t *testing.T, scenario string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "website.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	settings := `{"user":{"email":"synthetic@example.invalid","dob":"1970-01-02","ids":{"slug":"synthetic-user","uuid":"synthetic-uuid","trakt":55}},"access_token":"must-not-import"}`
	profile := `{"ids":{"slug":"synthetic-user","trakt":55}}`
	if scenario == "wrong_account" {
		profile = `{"ids":{"slug":"other-user","trakt":55}}`
	}
	if scenario == "wrong_numeric_id" {
		profile = `{"ids":{"slug":"synthetic-user","trakt":56}}`
	}
	if scenario == "missing_field" {
		settings = `{"user":{"email":"synthetic@example.invalid","ids":{"slug":"synthetic-user","uuid":"synthetic-uuid","trakt":55}}}`
	}
	for _, entry := range []struct{ name, data string }{{"user-settings.json", settings}, {"user-profile.json", profile}, {"../not-extracted.txt", "ignored"}} {
		w, err := z.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "duplicate" {
		w, err := z.Create("user-settings.json")
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(settings))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func importedSupplement(t *testing.T) *WebsiteSupplement {
	t.Helper()
	s, err := ImportWebsiteSupplement(websiteFixture(t, ""), filepath.Join(t.TempDir(), "supplement.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSupplementImportAllowlistAndNoOverwrite(t *testing.T) {
	input := websiteFixture(t, "")
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "supplement.json")
	s, err := ImportWebsiteSupplement(input, output)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(before)
	if s.Source.SHA256 != hex.EncodeToString(h[:]) || s.Source.SnapshotTime != "unknown" {
		t.Fatal("incorrect provenance")
	}
	stored, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "must-not-import") || len(s.Fields) != 3 {
		t.Fatal("imported unrelated fields")
	}
	loaded, err := LoadWebsiteSupplement(output)
	if err != nil || loaded.Slug != "synthetic-user" {
		t.Fatalf("load failed: %v", err)
	}
	if _, err = ImportWebsiteSupplement(input, output); err == nil {
		t.Fatal("overwrote supplement")
	}
	after, _ := os.ReadFile(input)
	again, _ := os.ReadFile(output)
	if string(before) != string(after) || string(again) != string(stored) {
		t.Fatal("existing data changed")
	}
}

func TestSupplementImportRejectsInconsistentSources(t *testing.T) {
	for _, scenario := range []string{"wrong_account", "wrong_numeric_id", "missing_field", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "supplement.json")
			if _, err := ImportWebsiteSupplement(websiteFixture(t, scenario), output); err == nil {
				t.Fatal("invalid website source accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("invalid import created output")
			}
		})
	}
}

func supplementSource() *fakeSource {
	f := fixture()
	f.documentData = map[string]json.RawMessage{
		"user/settings": raw(`{"user":{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}}}`),
		"user/profile":  raw(`{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}}`),
		"user/stats":    raw(`{"total_plays":3}`),
	}
	return f
}

func TestSupplementedExportPreservesRawAPIAndProvenance(t *testing.T) {
	f := supplementSource()
	// Profile may omit UUID; the preceding settings snapshot must bind it.
	f.documentData["user/profile"] = raw(`{"ids":{"slug":"synthetic-user"}}`)
	o := options(t)
	o.WebsiteSupplement = importedSupplement(t)
	m, err := Export(f, o)
	if err != nil || !m.Complete {
		t.Fatalf("%+v %v", m, err)
	}
	if m.Mode != "website_supplemented" || m.Supplement == nil || len(m.Supplement.AppliedFields) != 4 || len(m.Supplement.RawAPIPaths) != 2 || m.Supplement.Freshness != "historical_values_not_revalidated" {
		t.Fatal("missing supplement provenance")
	}
	files := archive(t, o.Output)
	if strings.Contains(string(files["raw-api/user/settings.json"]), "email") || strings.Contains(string(files["raw-api/user/profile.json"]), `"trakt"`) {
		t.Fatal("raw API data modified")
	}
	var settings map[string]json.RawMessage
	json.Unmarshal(files["user/settings.json"], &settings)
	user, _ := decodeObject(settings["user"])
	if string(user["email"]) != string(o.WebsiteSupplement.Fields["email"]) || string(user["dob"]) != string(o.WebsiteSupplement.Fields["dob"]) {
		t.Fatal("supplement fields not merged")
	}
	for _, entry := range m.Files {
		digest := sha256.Sum256(files[entry.Path])
		if entry.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatalf("invalid merged/raw checksum: %s", entry.Path)
		}
	}
}

func TestSupplementDoesNotOverwriteLiveValues(t *testing.T) {
	s := importedSupplement(t)
	data := raw(`{"user":{"email":"newer@example.invalid","dob":null,"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid","trakt":55}}}`)
	merged, applied, err := s.merge("user/settings", data)
	if err != nil || len(applied) != 0 || !strings.Contains(string(merged), "newer@example.invalid") || !strings.Contains(string(merged), `"dob":null`) {
		t.Fatalf("replaced current API values: %v", err)
	}
}

func TestSupplementRejectsWrongLiveAccount(t *testing.T) {
	for _, scenario := range []string{"slug", "uuid", "missing_uuid", "numeric_id"} {
		t.Run(scenario, func(t *testing.T) {
			s := importedSupplement(t)
			data := `{"user":{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid"}}}`
			switch scenario {
			case "slug":
				data = strings.ReplaceAll(data, "synthetic-user", "other-user")
			case "uuid":
				data = strings.ReplaceAll(data, "synthetic-uuid", "other-uuid")
			case "missing_uuid":
				data = `{"user":{"ids":{"slug":"synthetic-user"}}}`
			case "numeric_id":
				data = `{"user":{"ids":{"slug":"synthetic-user","uuid":"synthetic-uuid","trakt":99}}}`
			}
			if _, _, err := s.merge("user/settings", raw(data)); err == nil {
				t.Fatal("mismatched account accepted")
			}
		})
	}
	f := supplementSource()
	o := options(t)
	o.WebsiteSupplement = importedSupplement(t)
	o.User = "other-user"
	if _, err := Export(f, o); err == nil {
		t.Fatal("wrong account accepted before reads")
	}
	if _, err := os.Stat(o.Output); !os.IsNotExist(err) {
		t.Fatal("created wrong-account ZIP")
	}
}

func TestSupplementBindingFailureRetainsIncompleteArchive(t *testing.T) {
	f := supplementSource()
	f.documentData["user/settings"] = raw(`{"user":{"ids":{"slug":"synthetic-user","uuid":"wrong"}}}`)
	o := options(t)
	o.WebsiteSupplement = importedSupplement(t)
	m, err := Export(f, o)
	if err == nil || m.Complete {
		t.Fatal("binding failure not surfaced")
	}
	var stored Manifest
	json.Unmarshal(archive(t, o.Output)["manifest.json"], &stored)
	if stored.Complete || len(stored.Errors) == 0 {
		t.Fatal("partial archive claimed complete")
	}
}

func TestSupplementLoaderRejectsUntrustedStructure(t *testing.T) {
	s := importedSupplement(t)
	valid, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{string(valid) + ` {}`, strings.TrimSuffix(string(valid), "}") + `,"extra":"not-allowed"}`, string(valid) + strings.Repeat(" ", 1<<20)} {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadWebsiteSupplement(path); err == nil {
			t.Fatal("accepted trailing, unknown or oversized input")
		}
	}
}
