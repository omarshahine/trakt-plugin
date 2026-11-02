package sources

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// All fixtures are synthetic: invented titles, ids and dates.

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"The Example Show":       "example show",
		"Café & Friends":         "cafe and friends",
		"Perú: Hidden Treasure!": "peru hidden treasure",
		"  A   Test  ":           "test",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigOptIn(t *testing.T) {
	var nilCfg *Config
	if ok, _ := nilCfg.Active(); ok {
		t.Fatal("missing config must be inactive")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.yaml")

	cfg, err := LoadConfig(path)
	if err != nil || cfg != nil {
		t.Fatalf("missing file should return (nil, nil), got %v %v", cfg, err)
	}

	write := func(s string) *Config {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if ok, _ := write("enabled: false\nsources: {netflix: {enabled: true}}\ncookie_command: echo\n").Active(); ok {
		t.Error("enabled: false must be inactive")
	}
	if ok, _ := write("enabled: true\ncookie_command: echo\n").Active(); ok {
		t.Error("no sources must be inactive")
	}
	if ok, _ := write("enabled: true\nsources: {netflix: {enabled: true}}\n").Active(); ok {
		t.Error("no cookie_command must be inactive")
	}
	c := write("enabled: true\ncookie_command: echo\nsince: 7d\ndeny: [\"Kids Show\"]\nallow: [\"New Drama\"]\nsources:\n  upnext: {enabled: true}\n  paramount: {enabled: true}\n")
	if ok, why := c.Active(); !ok {
		t.Fatalf("should be active: %s", why)
	}
	if d, _ := c.SinceDuration(); d != 7*24*time.Hour {
		t.Errorf("since = %v", d)
	}
	if !c.Denied("the kids show") || c.Denied("Grown-up Show") {
		t.Error("deny matching wrong")
	}
	if !c.Allowed("new drama") || c.Allowed("Kids Show") {
		t.Error("allow matching wrong")
	}
	if c.Sources.Paramount.MinProgress != 80 {
		t.Errorf("default min_progress = %d", c.Sources.Paramount.MinProgress)
	}
	if got := c.EnabledSources(); len(got) != 2 || got[0] != "upnext" || got[1] != "paramount" {
		t.Errorf("EnabledSources = %v", got)
	}
}

func TestParseCookies(t *testing.T) {
	j, _ := ParseCookies([]byte(`[{"name":"a","value":"1"},{"Name":"b","Value":"2"},{"name":"a","value":"dup"}]`))
	if j["a"] != "1" || j["b"] != "2" {
		t.Errorf("json jar = %v", j)
	}
	h, _ := ParseCookies([]byte("Cookie: x=1; y=2=3\n"))
	if h["x"] != "1" || h["y"] != "2=3" {
		t.Errorf("header jar = %v", h)
	}
	if got := (Cookies{"b": "2", "a": "1"}).Header(); got != "a=1; b=2" {
		t.Errorf("Header() = %q", got)
	}
}

func TestUpNextInference(t *testing.T) {
	body := []byte(`{"data":{"channels":{"c1":{"name":"Partner App"}},"shelf":{"nextToken":"tok","items":[
	 {"type":"Episode","showTitle":"Show A","seasonNumber":2,"episodeNumber":4,"context":"NextEpisode","timestamp":1790000000000,"playable":{"channelId":"c1"}},
	 {"type":"Episode","showTitle":"Show B","seasonNumber":1,"episodeNumber":3,"context":"Continue","timestamp":1790000000000},
	 {"type":"Episode","showTitle":"Show C","seasonNumber":1,"episodeNumber":1,"context":"Continue","timestamp":1790000000000},
	 {"type":"Movie","title":"Some Movie","context":"Continue","timestamp":1790000000000}]}}}`)
	items, ch, next, err := ParseUpNext(body)
	if err != nil {
		t.Fatal(err)
	}
	if next != "tok" || ch["c1"] != "Partner App" || items[0].ChannelID != "c1" || items[1].ChannelID != "" {
		t.Fatalf("parse: next=%q ch=%v items=%+v", next, ch, items)
	}
	w := InferUpNext(items)
	// Show A S2E1-3, Show B S1E1-2; Show C (E1) and the movie add nothing.
	if len(w) != 5 {
		t.Fatalf("want 5 inferred watches, got %d: %+v", len(w), w)
	}
	if w[0].ShowTitle != "Show A" || w[0].Season != 2 || w[0].Episode != 1 || !w[0].Inferred {
		t.Errorf("first = %+v", w[0])
	}
	if w[0].WatchedAt.UnixMilli() != 1790000000000 {
		t.Errorf("timestamp not carried: %v", w[0].WatchedAt)
	}
}

func TestNetflixParsing(t *testing.T) {
	page := `<script>reactContext = {"models":{"userInfo":{"data":{"membershipStatus":"CURRENT_MEMBER","name":"Guest Profile","guid":"GUESTGUID0000000000000000"}}},` +
		`"profiles":[{"firstName":"Guest Profile","guid":"GUESTGUID0000000000000000"},{"firstName":"Alex","guid":"ALEXGUID00000000000000000"}],` +
		`"viewingActivity":{"viewedItems":[` +
		`{"date":0,"dateStr":"10/31/26","title":"Season 1: \x22Pilot Night\x22","seriesTitle":"Example Drama","seasonDescriptor":"Season 1","episodeTitle":"\x22Pilot Night\x22"},` +
		`{"date":0,"dateStr":"6/18/26","title":"Example Movie: The Sequel","movieID":1}` +
		`]}}</script>`
	if got := NetflixActiveProfile(page); got != "Guest Profile" {
		t.Errorf("active profile = %q", got)
	}
	if p := NetflixProfiles(page); p["Alex"] != "ALEXGUID00000000000000000" || p["Guest Profile"] != "GUESTGUID0000000000000000" {
		t.Errorf("profiles = %v", p)
	}
	w, err := ParseNetflixActivity(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 2 {
		t.Fatalf("want 2, got %+v", w)
	}
	if w[0].Kind != KindEpisode || w[0].ShowTitle != "Example Drama" || w[0].Season != 1 || w[0].EpisodeTitle != "Pilot Night" || w[0].Episode != 0 {
		t.Errorf("episode = %+v", w[0])
	}
	if w[0].WatchedAt.Format("2006-01-02 15:04") != "2026-10-31 20:00" {
		t.Errorf("date = %v", w[0].WatchedAt)
	}
	if w[1].Kind != KindMovie || w[1].MovieTitle != "Example Movie: The Sequel" {
		t.Errorf("movie = %+v", w[1])
	}
}

func TestParamount(t *testing.T) {
	body := []byte(`{"success":true,"result":{"data":[
	 {"title":"Show X","showSeriesTitle":"Show X","seasonNumber":"2","episodeNumber":"3","progress":91,"isMovie":false},
	 {"title":"Show Y","showSeriesTitle":"Show Y","seasonNumber":1,"episodeNumber":7,"progress":"38","isMovie":false},
	 {"title":"A Film","seasonNumber":"","episodeNumber":"","progress":85,"isMovie":true}]}}`)
	w, err := ParseParamount(body, 80)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 2 || w[0].ShowTitle != "Show X" || w[0].Season != 2 || w[0].Episode != 3 || w[1].Kind != KindMovie {
		t.Errorf("got %+v", w)
	}
}

func TestHBOCompletion(t *testing.T) {
	d := hboDoc{}
	add := func(typ string, s, e int, done bool, vt string) {
		var x hboEntity
		x.Type = typ
		x.Attributes.SeasonNumber, x.Attributes.EpisodeNumber, x.Attributes.VideoType = s, e, vt
		if done {
			x.Attributes.ViewingHistory = &struct {
				Completed bool `json:"completed"`
			}{true}
		}
		d.Included = append(d.Included, x)
	}
	add("video", 1, 1, true, "EPISODE")
	add("video", 1, 2, false, "EPISODE")
	add("video", 0, 0, true, "MOVIE")
	add("show", 1, 3, true, "")
	got := d.completedEpisodes()
	if len(got) != 1 || got[0] != [2]int{1, 1} {
		t.Errorf("completed = %v", got)
	}
}

func TestUpNextOnlyConfirmsPreviousEpisode(t *testing.T) {
	items := []upNextItem{
		{Type: "Episode", ShowTitle: "Show A", SeasonNumber: 1, EpisodeNumber: 4, Context: "NextEpisode"},
		{Type: "Episode", ShowTitle: "Show B", SeasonNumber: 1, EpisodeNumber: 3, Context: "Continue"},
	}
	var confirmed, review []string
	for _, w := range InferUpNext(items) {
		k := fmt.Sprintf("%s E%d", w.ShowTitle, w.Episode)
		if w.NeedsReview {
			review = append(review, k)
		} else {
			confirmed = append(confirmed, k)
		}
	}
	// Only Show A E3 is proven finished; skipped/continued ranges need review.
	if strings.Join(confirmed, ",") != "Show A E3" {
		t.Errorf("confirmed = %v", confirmed)
	}
	if strings.Join(review, ",") != "Show A E1,Show A E2,Show B E1,Show B E2" {
		t.Errorf("review = %v", review)
	}
}

func TestNormalizeKeepsNonLatinDistinct(t *testing.T) {
	a, b := Normalize("東京"), Normalize("京都")
	if a == "" || b == "" || a == b {
		t.Fatalf("non-Latin titles collapsed: %q %q", a, b)
	}
	c := &Config{Allow: []string{"東京"}, Deny: []string{"!!!"}}
	if c.Allowed("京都") || !c.Allowed("東京") {
		t.Error("allow matched the wrong non-Latin title")
	}
	if c.Denied("???") {
		t.Error("empty normalized titles must never match")
	}
}

func TestParamountRejectsBadNumbers(t *testing.T) {
	body := []byte(`{"result":{"data":[
	 {"showSeriesTitle":"NaN Show","seasonNumber":1,"episodeNumber":2,"progress":"NaN"},
	 {"showSeriesTitle":"Inf Show","seasonNumber":1,"episodeNumber":2,"progress":"+Inf"},
	 {"showSeriesTitle":"Frac Show","seasonNumber":1.5,"episodeNumber":2,"progress":95},
	 {"showSeriesTitle":"Over Show","seasonNumber":1,"episodeNumber":2,"progress":150},
	 {"showSeriesTitle":"Good Show","seasonNumber":1,"episodeNumber":2,"progress":95}]}}`)
	w, err := ParseParamount(body, 80)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 1 || w[0].ShowTitle != "Good Show" {
		t.Errorf("got %+v", w)
	}
}
