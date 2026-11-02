// Package sources collects "what did I watch" signals from streaming services
// using the user's own signed-in browser sessions, for `trakt-cli sync`.
//
// Everything here is opt-in: nothing runs unless the user creates a sync
// config (see config.go). Sources only read; writing to Trakt happens in cmd.
package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Kind is what a Watch refers to.
type Kind string

const (
	KindEpisode Kind = "episode"
	KindMovie   Kind = "movie"
)

// Watch is one item a source believes the user finished.
type Watch struct {
	Kind   Kind   `json:"kind"`
	Source string `json:"source"`

	ShowTitle string `json:"show_title,omitempty"`
	Season    int    `json:"season,omitempty"`
	// Episode is 0 when the service only reports EpisodeTitle (Netflix);
	// the resolver fills it from Trakt's season listing.
	Episode      int    `json:"episode,omitempty"`
	EpisodeTitle string `json:"episode_title,omitempty"`

	MovieTitle string `json:"movie_title,omitempty"`
	Year       int    `json:"year,omitempty"`

	// WatchedAt is when the service says it happened. Zero means unknown;
	// the caller stamps it with the run time.
	WatchedAt time.Time `json:"watched_at"`
	// Inferred marks episodes implied by a "next episode" pointer rather
	// than reported directly.
	Inferred bool `json:"inferred,omitempty"`
	// NeedsReview means the evidence is too weak to write automatically,
	// even for a show already in history; it goes to pending_review unless
	// the title is approved.
	NeedsReview bool `json:"needs_review,omitempty"`
}

// Title is the show title for episodes and the movie title for movies.
func (w Watch) Title() string {
	if w.Kind == KindMovie {
		return w.MovieTitle
	}
	return w.ShowTitle
}

// Source is one streaming service connector.
type Source interface {
	Name() string
	Fetch(ctx context.Context) ([]Watch, error)
}

// UserAgent is a desktop browser UA; several services reject unknown agents.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

// httpClient never follows redirects on its own so callers can see
// sign-in bounces and capture Set-Cookie on profile switches.
var httpClient = &http.Client{
	Timeout: 45 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type response struct {
	Status  int
	Body    []byte
	Header  http.Header
	Request *http.Request
}

// fetch performs a GET (following up to 5 redirects manually) and returns
// the final response. Truncated bodies are returned as-is: some services
// close chunked HTML early and the data we need is near the top.
func fetch(ctx context.Context, url string, headers map[string]string) (*response, error) {
	for hop := 0; hop < 6; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil && len(body) == 0 {
			return nil, readErr
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.Header.Get("Location") != "" {
			next, err := resp.Request.URL.Parse(resp.Header.Get("Location"))
			if err != nil {
				return nil, err
			}
			url = next.String()
			continue
		}
		return &response{Status: resp.StatusCode, Body: body, Header: resp.Header, Request: resp.Request}, nil
	}
	return nil, fmt.Errorf("too many redirects fetching %s", redactURL(url))
}

func redactURL(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

var (
	// Unicode letters and digits survive, so non-Latin titles stay distinct.
	nonAlnum  = regexp.MustCompile(`[^\p{L}\p{N} ]+`)
	articles  = regexp.MustCompile(`\b(the|a|an)\b`)
	spaceRuns = regexp.MustCompile(`\s+`)
)

// Normalize folds a title for matching across services: accents stripped,
// lowercase, "&" as "and", punctuation and leading articles removed.
func Normalize(s string) string {
	out := strings.ToLower(accentFolder.Replace(s))
	out = strings.ReplaceAll(out, "&", " and ")
	out = nonAlnum.ReplaceAllString(out, " ")
	out = articles.ReplaceAllString(out, " ")
	return strings.TrimSpace(spaceRuns.ReplaceAllString(out, " "))
}

// accentFolder maps common Latin accented letters to ASCII, enough for
// streaming titles ("Perú", "Pokémon") without a unicode dependency.
var accentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"Á", "A", "À", "A", "Â", "A", "Ä", "A", "Ã", "A", "Å", "A",
	"é", "e", "è", "e", "ê", "e", "ë", "e", "É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "Í", "I", "Ì", "I", "Î", "I", "Ï", "I",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o",
	"Ó", "O", "Ò", "O", "Ô", "O", "Ö", "O", "Õ", "O", "Ø", "O",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "Ú", "U", "Ù", "U", "Û", "U", "Ü", "U",
	"ñ", "n", "Ñ", "N", "ç", "c", "Ç", "C", "ß", "ss",
	"’", "'", "‘", "'", "“", "\"", "”", "\"", "–", "-", "—", "-",
)
