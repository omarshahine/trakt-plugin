package backup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"
	"time"
)

// WebsiteSupplement deliberately stores only the three website-only fields,
// account binding, and provenance. It never imports arbitrary account settings.
type WebsiteSupplement struct {
	Version int                        `json:"version"`
	Source  SupplementSource           `json:"source"`
	Slug    string                     `json:"account_slug"`
	UUID    string                     `json:"account_uuid"`
	Fields  map[string]json.RawMessage `json:"fields"`
}

type SupplementSource struct {
	Kind       string    `json:"kind"`
	SHA256     string    `json:"sha256"`
	ImportedAt time.Time `json:"imported_at"`
	// Import time is not the time the historical website snapshot was made.
	SnapshotTime string `json:"snapshot_time"`
}

type SupplementProvenance struct {
	Source        SupplementSource `json:"source"`
	Freshness     string           `json:"freshness"`
	AppliedFields []string         `json:"applied_fields"`
	RawAPIPaths   []string         `json:"raw_api_paths"`
}

func (s *WebsiteSupplement) validate() error {
	if s.Version != 1 || strings.TrimSpace(s.Slug) == "" || strings.TrimSpace(s.UUID) == "" || len(s.Fields) != 3 || s.Source.Kind != "trakt_website_export" || s.Source.ImportedAt.IsZero() || s.Source.SnapshotTime != "unknown" {
		return fmt.Errorf("invalid website supplement metadata")
	}
	h, err := hex.DecodeString(s.Source.SHA256)
	if err != nil || len(h) != 32 {
		return fmt.Errorf("invalid supplement source checksum")
	}
	var email string
	if err := json.Unmarshal(s.Fields["email"], &email); err != nil || strings.TrimSpace(email) == "" {
		return fmt.Errorf("invalid supplement email")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return fmt.Errorf("invalid supplement email")
	}
	dob, ok := s.Fields["dob"]
	if !ok {
		return fmt.Errorf("missing supplement date of birth")
	}
	if string(dob) != "null" {
		var date string
		if err := json.Unmarshal(dob, &date); err != nil {
			return fmt.Errorf("invalid supplement date of birth")
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("invalid supplement date of birth")
		}
	}
	if _, err := positiveIdentity(s.Fields["trakt"]); err != nil {
		return fmt.Errorf("invalid supplement numeric user ID")
	}
	return nil
}

func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return object, nil
}

// ImportWebsiteSupplement reads two exact ZIP members without extracting paths.
// Both members must agree on account identity. The ZIP itself is unchanged.
func ImportWebsiteSupplement(zipPath, output string) (*WebsiteSupplement, error) {
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open website ZIP: %w", err)
	}
	defer z.Close()
	docs := map[string][]byte{}
	for _, entry := range z.File {
		if entry.Name != "user-settings.json" && entry.Name != "user-profile.json" {
			continue
		}
		if _, exists := docs[entry.Name]; exists {
			return nil, fmt.Errorf("duplicate website snapshot member")
		}
		if entry.UncompressedSize64 > 1<<20 {
			return nil, fmt.Errorf("website snapshot member too large")
		}
		r, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(r, (1<<20)+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || len(data) > 1<<20 {
			return nil, fmt.Errorf("cannot read bounded website snapshot")
		}
		docs[entry.Name] = data
	}
	settings, err := decodeObject(docs["user-settings.json"])
	if err != nil {
		return nil, fmt.Errorf("missing/invalid website settings")
	}
	user, err := decodeObject(settings["user"])
	if err != nil {
		return nil, fmt.Errorf("missing website settings user")
	}
	profile, err := decodeObject(docs["user-profile.json"])
	if err != nil {
		return nil, fmt.Errorf("missing/invalid website profile")
	}
	type ids struct {
		Slug  string          `json:"slug"`
		UUID  string          `json:"uuid"`
		Trakt json.RawMessage `json:"trakt"`
	}
	var a, b ids
	if json.Unmarshal(user["ids"], &a) != nil || json.Unmarshal(profile["ids"], &b) != nil || a.Slug != b.Slug || a.Slug == "" || a.UUID == "" || (b.UUID != "" && a.UUID != b.UUID) {
		return nil, fmt.Errorf("website snapshots have mismatched/missing account identities")
	}
	an, ae := positiveIdentity(a.Trakt)
	bn, be := positiveIdentity(b.Trakt)
	if ae != nil || be != nil || an != bn {
		return nil, fmt.Errorf("website snapshots disagree on numeric user ID")
	}
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, hashErr := io.Copy(h, f)
	closeErr := f.Close()
	if hashErr != nil || closeErr != nil {
		return nil, fmt.Errorf("cannot checksum source website ZIP")
	}
	s := &WebsiteSupplement{Version: 1, Source: SupplementSource{Kind: "trakt_website_export", SHA256: hex.EncodeToString(h.Sum(nil)), ImportedAt: time.Now().UTC(), SnapshotTime: "unknown"}, Slug: a.Slug, UUID: a.UUID, Fields: map[string]json.RawMessage{"email": user["email"], "dob": user["dob"], "trakt": a.Trakt}}
	if err := s.validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	f, err = os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(append(data, '\n'))
	syncErr := f.Sync()
	closeErr = f.Close()
	for _, err := range []error{writeErr, syncErr, closeErr} {
		if err != nil {
			return nil, fmt.Errorf("write supplement: %w", err)
		}
	}
	return s, nil
}

func LoadWebsiteSupplement(path string) (*WebsiteSupplement, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("supplement file is unreadable or too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var s WebsiteSupplement
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("invalid supplement JSON")
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("trailing supplement data")
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Merge fills absent keys only. An explicit null or newer API value wins.
// The exporter validates settings (mandatory UUID) before profile. Profile may
// omit UUID, but must match the same unique account slug and any returned ID.
func (s *WebsiteSupplement) merge(resource string, data json.RawMessage) (json.RawMessage, []string, error) {
	object, err := decodeObject(data)
	if err != nil {
		return nil, nil, err
	}
	user := object
	if resource == "user/settings" {
		user, err = decodeObject(object["user"])
		if err != nil {
			return nil, nil, fmt.Errorf("settings lack account binding")
		}
	}
	ids, err := decodeObject(user["ids"])
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot lacks account identity")
	}
	var slug, uuid string
	if json.Unmarshal(ids["slug"], &slug) != nil || slug != s.Slug {
		return nil, nil, fmt.Errorf("supplement account slug does not match live snapshot")
	}
	if raw, ok := ids["uuid"]; ok {
		if json.Unmarshal(raw, &uuid) != nil || uuid != s.UUID {
			return nil, nil, fmt.Errorf("supplement account UUID does not match live snapshot")
		}
	} else if resource == "user/settings" {
		return nil, nil, fmt.Errorf("live snapshot lacks UUID for supplement account binding")
	}
	if raw, ok := ids["trakt"]; ok {
		a, ae := positiveIdentity(raw)
		b, be := positiveIdentity(s.Fields["trakt"])
		if ae != nil || be != nil || a != b {
			return nil, nil, fmt.Errorf("numeric account ID conflicts with supplement")
		}
	}
	applied := []string{}
	if resource == "user/settings" {
		for _, field := range []string{"email", "dob"} {
			if _, exists := user[field]; !exists {
				user[field] = s.Fields[field]
				applied = append(applied, resource+":user."+field)
			}
		}
	}
	if _, exists := ids["trakt"]; !exists {
		ids["trakt"] = s.Fields["trakt"]
		path := resource + ":ids.trakt"
		if resource == "user/settings" {
			path = resource + ":user.ids.trakt"
		}
		applied = append(applied, path)
	}
	user["ids"], err = json.Marshal(ids)
	if err != nil {
		return nil, nil, err
	}
	if resource == "user/settings" {
		object["user"], err = json.Marshal(user)
		if err != nil {
			return nil, nil, err
		}
	} else {
		object = user
	}
	merged, err := json.Marshal(object)
	return merged, applied, err
}
