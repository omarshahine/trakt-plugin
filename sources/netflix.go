package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Netflix reads the account's Viewing Activity page for one profile. The page
// server-renders the latest items as "M/D/YY" plus a title link such as
// `Series: Season 2: "Episode"` (older builds embedded a viewedItems JSON
// array instead; both are parsed). Episodes come back by title, so the
// resolver maps them to numbers.
type Netflix struct {
	Cookies CookieLoader
	// Profile is the profile first name to read (accounts are shared).
	// Empty reads whatever profile the session is on.
	Profile string
}

func (n *Netflix) Name() string { return "netflix" }

// /viewingactivity now redirects here (2026-11); request it directly.
const netflixActivity = "https://www.netflix.com/settings/viewed"

func (n *Netflix) Fetch(ctx context.Context) ([]Watch, error) {
	jar, err := n.Cookies(ctx, ".netflix.com")
	if err != nil {
		return nil, err
	}
	body, err := n.page(ctx, jar)
	if err != nil {
		return nil, err
	}
	if n.Profile != "" && !strings.EqualFold(NetflixActiveProfile(body), n.Profile) {
		guid := NetflixProfiles(body)[n.Profile]
		if guid == "" {
			return nil, fmt.Errorf("netflix: profile %q not found on this account", n.Profile)
		}
		if err := n.switchProfile(ctx, jar, guid); err != nil {
			return nil, err
		}
		if body, err = n.page(ctx, jar); err != nil {
			return nil, err
		}
		if got := NetflixActiveProfile(body); !strings.EqualFold(got, n.Profile) {
			return nil, fmt.Errorf("netflix: still on profile %q after switching to %q", got, n.Profile)
		}
	}
	return ParseNetflixActivity(body)
}

func (n *Netflix) page(ctx context.Context, jar Cookies) (string, error) {
	resp, err := fetch(ctx, netflixActivity, map[string]string{"Cookie": jar.Header()})
	if err != nil {
		return "", err
	}
	if resp.Status != 200 || strings.Contains(resp.Request.URL.Path, "login") {
		return "", fmt.Errorf("netflix: not signed in (HTTP %d)", resp.Status)
	}
	return string(resp.Body), nil
}

// switchProfile calls the same endpoint the profile picker uses, then merges
// the re-issued session cookies into jar (in memory only).
func (n *Netflix) switchProfile(ctx context.Context, jar Cookies, guid string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.netflix.com/SwitchProfile?tkn="+guid, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Cookie", jar.Header())
	resp, err := httpClient.Do(req)
	if err != nil {
		return redactErr(err)
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		jar[c.Name] = c.Value
	}
	return nil
}

var (
	hexEscape      = regexp.MustCompile(`\\x([0-9A-Fa-f]{2})`)
	nfFirstName    = regexp.MustCompile(`"firstName":"([^"]{1,60})"`)
	nfGUID         = regexp.MustCompile(`"guid":"([A-Z0-9]{20,40})"`)
	nfUserInfoName = regexp.MustCompile(`"userInfo":\{"data":\{[^{}]*?"name":"([^"]+)"`)
	nfSeasonNum    = regexp.MustCompile(`(\d+)`)
)

// decodeHex turns the page's \xNN escapes into characters (for regex scans).
func decodeHex(s string) string {
	return hexEscape.ReplaceAllStringFunc(s, func(m string) string {
		v, _ := strconv.ParseUint(m[2:], 16, 8)
		return string(rune(v))
	})
}

// NetflixProfiles maps profile first names to their GUIDs.
func NetflixProfiles(page string) map[string]string {
	p := decodeHex(page)
	out := map[string]string{}
	for _, m := range nfFirstName.FindAllStringSubmatchIndex(p, -1) {
		name := p[m[2]:m[3]]
		lo, hi := m[0]-300, m[1]+300
		if lo < 0 {
			lo = 0
		}
		if hi > len(p) {
			hi = len(p)
		}
		// Take the guid nearest the name: neighbouring profile objects sit
		// within the window too, so "first match" can pick the wrong one.
		best, bestDist := "", -1
		for _, g := range nfGUID.FindAllStringSubmatchIndex(p[lo:hi], -1) {
			start, end := lo+g[0], lo+g[1]
			dist := start - m[1] // guid after the name
			if start < m[0] {
				dist = m[0] - end // guid before the name
			}
			if bestDist < 0 || dist < bestDist {
				best, bestDist = p[lo+g[2]:lo+g[3]], dist
			}
		}
		if best != "" {
			if _, seen := out[name]; !seen {
				out[name] = best
			}
		}
	}
	return out
}

// NetflixActiveProfile returns the name of the profile the page was rendered for.
func NetflixActiveProfile(page string) string {
	if m := nfUserInfoName.FindStringSubmatch(decodeHex(page)); m != nil {
		return m[1]
	}
	return ""
}

type netflixItem struct {
	Date             int64  `json:"date"`
	DateStr          string `json:"dateStr"`
	Title            string `json:"title"`
	SeriesTitle      string `json:"seriesTitle"`
	SeasonDescriptor string `json:"seasonDescriptor"`
	EpisodeTitle     string `json:"episodeTitle"`
}

var (
	// One rendered list item: <span>11/8/26</span><a ...title-link...>Title</a>
	nfHTMLItem = regexp.MustCompile(`data-uia="viewing-activity-page\+list-item\+description"[^>]*>\s*<span[^>]*>([0-9/]+)</span>\s*<a[^>]*>([^<]*)</a>`)
	// `Series: Season 2: "Episode"` / `Series: Limited Series: "Episode"`;
	// the series name may itself contain colons, so match it lazily.
	nfHTMLEpisode = regexp.MustCompile(`^(.+?): ([^:"]+): "(.*)"$`)
	// `Series: "Episode"` with no season descriptor.
	nfHTMLBareEpisode = regexp.MustCompile(`^(.+?): "(.*)"$`)
)

// ParseNetflixActivity extracts the viewing history from the page: the
// server-rendered list items, or the legacy embedded viewedItems array.
func ParseNetflixActivity(page string) ([]Watch, error) {
	i := strings.Index(page, `"viewedItems"`)
	if i < 0 {
		return parseNetflixHTML(page)
	}
	j := strings.IndexByte(page[i:], '[')
	if j < 0 {
		return nil, fmt.Errorf("netflix: viewedItems not an array")
	}
	start := i + j
	depth, end := 0, -1
	inStr, esc := false, false
	for k := start; k < len(page); k++ {
		c := page[k]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = k + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("netflix: viewedItems truncated")
	}
	raw := hexEscape.ReplaceAllString(page[start:end], `\u00$1`)
	var items []netflixItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("netflix: decode viewedItems: %w", err)
	}

	var out []Watch
	for _, it := range items {
		when := netflixWhen(it)
		if it.SeriesTitle != "" {
			season := 1
			if m := nfSeasonNum.FindString(it.SeasonDescriptor); m != "" {
				season, _ = strconv.Atoi(m)
			}
			out = append(out, Watch{
				Kind:         KindEpisode,
				Source:       "netflix",
				ShowTitle:    it.SeriesTitle,
				Season:       season,
				EpisodeTitle: strings.Trim(it.EpisodeTitle, `"`),
				WatchedAt:    when,
			})
			continue
		}
		if it.Title == "" {
			continue
		}
		out = append(out, Watch{Kind: KindMovie, Source: "netflix", MovieTitle: it.Title, WatchedAt: when})
	}
	return out, nil
}

// parseNetflixHTML reads the list items rendered on /settings/viewed. Only
// the first page (about six titles) is server-rendered; "Show More" fetches
// the rest client-side, which a 4-hourly sync does not need.
func parseNetflixHTML(page string) ([]Watch, error) {
	matches := nfHTMLItem.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		if strings.Contains(page, `viewing-activity-page`) {
			return nil, nil // the page rendered, the profile has no history
		}
		return nil, fmt.Errorf("netflix: viewing activity not found (page layout changed?)")
	}
	var out []Watch
	for _, m := range matches {
		when := netflixWhen(netflixItem{DateStr: m[1]})
		title := strings.TrimSpace(html.UnescapeString(m[2]))
		if title == "" {
			continue
		}
		season, show, episode := 1, "", ""
		if e := nfHTMLEpisode.FindStringSubmatch(title); e != nil {
			show, episode = e[1], e[3]
			if n := nfSeasonNum.FindString(e[2]); n != "" {
				season, _ = strconv.Atoi(n)
			}
		} else if e := nfHTMLBareEpisode.FindStringSubmatch(title); e != nil {
			show, episode = e[1], e[2]
		}
		if show != "" {
			out = append(out, Watch{
				Kind:         KindEpisode,
				Source:       "netflix",
				ShowTitle:    show,
				Season:       season,
				EpisodeTitle: episode,
				WatchedAt:    when,
			})
			continue
		}
		out = append(out, Watch{Kind: KindMovie, Source: "netflix", MovieTitle: title, WatchedAt: when})
	}
	return out, nil
}

// netflixWhen prefers the epoch-millis date; falls back to dateStr (M/D/YY)
// at 20:00 local time, since Netflix reports a day, not a time.
func netflixWhen(it netflixItem) time.Time {
	if it.Date > 0 {
		t := time.UnixMilli(it.Date)
		if t.Hour() == 0 && t.Minute() == 0 {
			t = t.Add(20 * time.Hour)
		}
		return t
	}
	if t, err := time.ParseInLocation("1/2/06", it.DateStr, time.Local); err == nil {
		return t.Add(20 * time.Hour)
	}
	return time.Time{}
}
