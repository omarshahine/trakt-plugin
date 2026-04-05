package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type APIClient struct {
	// The url of the API endpoint
	Endpoint string
	// The client for accessing the API
	Client *http.Client
	// The credentials
	Credentials Credentials
}

type Credentials struct {
	ClientID     string `yaml:"client-id"`
	ClientSecret string `yaml:"client-secret"`
	AccessToken  string `yaml:"access-token"`
	// UserSlug caches the authenticated user's slug so read-only commands
	// (watchlist, history, progress) don't have to re-fetch /users/settings
	// on every invocation. Populated by GetUserSlug() on first use and
	// refreshed by the auth flow. Note: a duplicate of this struct exists
	// in cmd/auth.go for yaml marshaling on write; keep the two in sync.
	UserSlug string `yaml:"user-slug,omitempty"`
}

// RateLimitError is returned by doRequest when Trakt (or Cloudflare's edge)
// responds with HTTP 429. It captures the Retry-After header so callers and
// plugin wrappers can back off structurally instead of parsing stderr.
type RateLimitError struct {
	Path              string
	RetryAfterSeconds int
	Status            string
}

func (e *RateLimitError) Error() string {
	if e.RetryAfterSeconds > 0 {
		return fmt.Sprintf("rate limited by trakt api: %s (path=%s retry_after=%d)", e.Status, e.Path, e.RetryAfterSeconds)
	}
	return fmt.Sprintf("rate limited by trakt api: %s (path=%s)", e.Status, e.Path)
}

// Create a new API client for the given API version.
// Silently returns an empty-credentials client if ~/.trakt.yaml doesn't exist
// (the auth command doesn't need credentials to initiate the device flow).
func NewAPIClient() APIClient {
	var creds Credentials

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	out, err := os.ReadFile(homeDir + "/.trakt.yaml")
	if err == nil {
		if err := yaml.Unmarshal(out, &creds); err != nil {
			logrus.WithError(err).Error("Failed to parse ~/.trakt.yaml")
		}
	}

	return NewAPIClientWithCredentials(creds)
}

// NewAPIClientWithCredentials constructs an APIClient with explicit credentials
// instead of loading them from ~/.trakt.yaml. Used by the auth flow to prime
// the user-slug cache with a just-issued access token before persisting it.
func NewAPIClientWithCredentials(creds Credentials) APIClient {
	return APIClient{
		Endpoint: "https://api.trakt.tv",
		Client: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				IdleConnTimeout: 5 * time.Second,
			},
		},
		Credentials: creds,
	}
}

type AuthDeviceCodeReq struct {
	ClientID string `json:"client_id"`
}

type AuthDeviceCodeResp struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type requestParams struct {
	method     string
	path       string
	body       interface{}
	auth       bool
	pagination PaginationsParams
	query      map[string]string
}

func (c *APIClient) doRequest(params requestParams) (*http.Response, error) {
	req, err := http.NewRequest(params.method, c.Endpoint+params.path, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("trakt-api-version", "2")

	if params.body != nil {
		req.Header.Add("Content-Type", "application/json")
		body, err := json.Marshal(params.body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	q := req.URL.Query()
	if params.pagination.Page != 0 {
		q.Add("page", fmt.Sprintf("%d", params.pagination.Page))
	}
	if params.pagination.Limit != 0 {
		q.Add("limit", fmt.Sprintf("%d", params.pagination.Limit))
	}
	for k, v := range params.query {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()

	if params.auth {
		req.Header.Add("trakt-api-key", c.Credentials.ClientID)
		req.Header.Add("Authorization", "Bearer "+c.Credentials.AccessToken)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}

	// Detect rate limits (Trakt app-level or Cloudflare 1015 edge) and
	// surface the Retry-After value as a typed error so wrappers can back
	// off structurally instead of re-hitting the endpoint immediately.
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := 0
		if v := resp.Header.Get("Retry-After"); v != "" {
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				retryAfter = n
			}
		}
		resp.Body.Close()
		return nil, &RateLimitError{
			Path:              params.path,
			RetryAfterSeconds: retryAfter,
			Status:            resp.Status,
		}
	}

	return resp, nil
}

// writeCredentials persists c.Credentials to ~/.trakt.yaml. Best-effort:
// write errors are logged but not returned, because the read commands that
// trigger a slug refresh should not fail just because disk persistence fails.
func (c *APIClient) writeCredentials() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logrus.WithError(err).Warn("Failed to resolve home directory for trakt credential cache")
		return
	}
	data, err := yaml.Marshal(&c.Credentials)
	if err != nil {
		logrus.WithError(err).Warn("Failed to marshal trakt credentials for cache write")
		return
	}
	if err := os.WriteFile(homeDir+"/.trakt.yaml", data, 0600); err != nil {
		logrus.WithError(err).Warn("Failed to write trakt credential cache ~/.trakt.yaml")
	}
}

// GetUserSlug returns the authenticated user's trakt slug, using an on-disk
// cache in ~/.trakt.yaml to avoid an API round-trip on every read command.
// On cache miss it fetches /users/settings, persists the slug, and returns it.
// Stable for the lifetime of the account — invalidated only by the auth flow.
func (c *APIClient) GetUserSlug() (string, error) {
	if c.Credentials.UserSlug != "" {
		return c.Credentials.UserSlug, nil
	}
	settings, err := c.GetUserSettings()
	if err != nil {
		return "", err
	}
	c.Credentials.UserSlug = settings.User.Ids.Slug
	c.writeCredentials()
	return c.Credentials.UserSlug, nil
}

func (c *APIClient) AuthDeviceCode(req *AuthDeviceCodeReq) (*AuthDeviceCodeResp, error) {
	var resp AuthDeviceCodeResp
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodPost,
		path:   "/oauth/device/code",
		body:   req,
		auth:   false,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

type AuthDeviceTokenReq struct {
	Code         string `json:"code"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type AuthDeviceTokenResp struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	CreatedAt    int    `json:"created_at"`
}

func (c *APIClient) AuthDeviceToken(req *AuthDeviceTokenReq) (*AuthDeviceTokenResp, error) {
	var resp AuthDeviceTokenResp
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodPost,
		path:   "/oauth/device/token",
		body:   req,
		auth:   false,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode == 200 {
		err = json.NewDecoder(httpResp.Body).Decode(&resp)
		if err != nil {
			return nil, err
		}
	}

	return &resp, nil
}

type UserHistory []HistoryItem

type HistoryItem struct {
	ID        int64     `json:"id"`
	WatchedAt time.Time `json:"watched_at"`
	Action    string    `json:"action"`
	Type      string    `json:"type"`
	Movie     struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt int    `json:"trakt"`
			Slug  string `json:"slug"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"movie,omitempty"`
	Episode struct {
		Season int    `json:"season"`
		Number int    `json:"number"`
		Title  string `json:"title"`
		Ids    struct {
			Trakt  int         `json:"trakt"`
			Tvdb   interface{} `json:"tvdb"`
			Imdb   string      `json:"imdb"`
			Tmdb   int         `json:"tmdb"`
			Tvrage interface{} `json:"tvrage"`
		} `json:"ids"`
	} `json:"episode,omitempty"`
	Show struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt  int         `json:"trakt"`
			Slug   string      `json:"slug"`
			Tvdb   int         `json:"tvdb"`
			Imdb   string      `json:"imdb"`
			Tmdb   int         `json:"tmdb"`
			Tvrage interface{} `json:"tvrage"`
		} `json:"ids"`
	} `json:"show,omitempty"`
}

type PaginationsParams struct {
	Page  int
	Limit int
}

type Pagination struct {
	Page      string `json:"page"`
	Limit     string `json:"limit"`
	PageCount string `json:"page_count"`
	ItemCount string `json:"item_count"`
}

func (c *APIClient) GetUserHistory(user string, historyType string, params PaginationsParams) (UserHistory, Pagination, error) {
	path := fmt.Sprintf("/users/%s/history", user)
	if historyType != "" {
		path += "/" + historyType
	}

	httpResp, err := c.doRequest(requestParams{
		method:     http.MethodGet,
		path:       path,
		body:       nil,
		auth:       true,
		pagination: params,
	})
	if err != nil {
		return nil, Pagination{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, Pagination{}, fmt.Errorf("failed to get user history: %s", httpResp.Status)
	}

	var resp UserHistory
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, Pagination{}, err
	}

	pagination := Pagination{
		Page:      httpResp.Header.Get("X-Pagination-Page"),
		Limit:     httpResp.Header.Get("X-Pagination-Limit"),
		PageCount: httpResp.Header.Get("X-Pagination-Page-Count"),
		ItemCount: httpResp.Header.Get("X-Pagination-Item-Count"),
	}

	return resp, pagination, nil
}

type UserSettings struct {
	User struct {
		Username string `json:"username"`
		Private  bool   `json:"private"`
		Name     string `json:"name"`
		Vip      bool   `json:"vip"`
		VipEp    bool   `json:"vip_ep"`
		Ids      struct {
			Slug string `json:"slug"`
			UUID string `json:"uuid"`
		} `json:"ids"`
		JoinedAt time.Time `json:"joined_at"`
		Location string    `json:"location"`
		About    string    `json:"about"`
		Gender   string    `json:"gender"`
		Age      int       `json:"age"`
		Images   struct {
			Avatar struct {
				Full string `json:"full"`
			} `json:"avatar"`
		} `json:"images"`
		VipOg    bool `json:"vip_og"`
		VipYears int  `json:"vip_years"`
	} `json:"user"`
	Account struct {
		Timezone   string `json:"timezone"`
		DateFormat string `json:"date_format"`
		Time24Hr   bool   `json:"time_24hr"`
		CoverImage string `json:"cover_image"`
	} `json:"account"`
	Connections struct {
		Facebook bool `json:"facebook"`
		Twitter  bool `json:"twitter"`
		Google   bool `json:"google"`
		Tumblr   bool `json:"tumblr"`
		Medium   bool `json:"medium"`
		Slack    bool `json:"slack"`
		Apple    bool `json:"apple"`
	} `json:"connections"`
	SharingText struct {
		Watching string `json:"watching"`
		Watched  string `json:"watched"`
		Rated    string `json:"rated"`
	} `json:"sharing_text"`
}

func (c *APIClient) GetUserSettings() (UserSettings, error) {
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodGet,
		path:   "/users/settings",
		body:   nil,
		auth:   true,
	})
	if err != nil {
		return UserSettings{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return UserSettings{}, fmt.Errorf("failed to get user settings: %s", httpResp.Status)
	}

	var resp UserSettings
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return UserSettings{}, err
	}

	return resp, nil
}

type WatchlistItem struct {
	Rank    int       `json:"rank"`
	ID      int64     `json:"id"`
	ListedAt time.Time `json:"listed_at"`
	Notes   string    `json:"notes"`
	Type    string    `json:"type"`
	Movie   *struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt int    `json:"trakt"`
			Slug  string `json:"slug"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"movie,omitempty"`
	Show *struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt int    `json:"trakt"`
			Slug  string `json:"slug"`
			Tvdb  int    `json:"tvdb"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"show,omitempty"`
}

func (c *APIClient) GetUserWatchlist(user string, listType string, params PaginationsParams) ([]WatchlistItem, Pagination, error) {
	path := fmt.Sprintf("/users/%s/watchlist", user)
	if listType != "" {
		path += "/" + listType
	}

	httpResp, err := c.doRequest(requestParams{
		method:     http.MethodGet,
		path:       path,
		body:       nil,
		auth:       true,
		pagination: params,
	})
	if err != nil {
		return nil, Pagination{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, Pagination{}, fmt.Errorf("failed to get watchlist: %s", httpResp.Status)
	}

	var resp []WatchlistItem
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, Pagination{}, err
	}

	pagination := Pagination{
		Page:      httpResp.Header.Get("X-Pagination-Page"),
		Limit:     httpResp.Header.Get("X-Pagination-Limit"),
		PageCount: httpResp.Header.Get("X-Pagination-Page-Count"),
		ItemCount: httpResp.Header.Get("X-Pagination-Item-Count"),
	}

	return resp, pagination, nil
}

type ShowProgress struct {
	Aired     int  `json:"aired"`
	Completed int  `json:"completed"`
	LastWatchedAt *time.Time `json:"last_watched_at"`
	NextEpisode *struct {
		Season int    `json:"season"`
		Number int    `json:"number"`
		Title  string `json:"title"`
		Ids    struct {
			Trakt int `json:"trakt"`
		} `json:"ids"`
	} `json:"next_episode"`
}

func (c *APIClient) GetShowProgress(showID int) (*ShowProgress, error) {
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodGet,
		path:   fmt.Sprintf("/shows/%d/progress/watched", showID),
		auth:   true,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get show progress: %s", httpResp.Status)
	}

	var resp ShowProgress
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

// WatchedShow represents a show from the /users/{id}/watched/shows endpoint.
// Each entry includes the show metadata and the number of plays/episodes watched.
type WatchedShow struct {
	Plays         int        `json:"plays"`
	LastWatchedAt *time.Time `json:"last_watched_at"`
	LastUpdatedAt *time.Time `json:"last_updated_at"`
	Show          *struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt int    `json:"trakt"`
			Slug  string `json:"slug"`
			Tvdb  int    `json:"tvdb"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"show"`
}

func (c *APIClient) GetUserWatched(user string, watchedType string) ([]WatchedShow, error) {
	path := fmt.Sprintf("/users/%s/watched/%s", user, watchedType)

	httpResp, err := c.doRequest(requestParams{
		method: http.MethodGet,
		path:   path,
		auth:   true,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get watched shows: %s", httpResp.Status)
	}

	var resp []WatchedShow
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

type SearchResult struct {
	Type  string  `json:"type"`
	Score float64 `json:"score"`
	Movie *struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt int    `json:"trakt"`
			Slug  string `json:"slug"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"movie,omitempty"`
	Show *struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt int    `json:"trakt"`
			Slug  string `json:"slug"`
			Tvdb  int    `json:"tvdb"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"show,omitempty"`
}

type SyncHistoryReq struct {
	Movies []SyncItem `json:"movies,omitempty"`
	Shows  []SyncItem `json:"shows,omitempty"`
}

type SyncItem struct {
	WatchedAt string `json:"watched_at,omitempty"`
	Ids       struct {
		Trakt int `json:"trakt"`
	} `json:"ids"`
}

type SyncHistoryResp struct {
	Added struct {
		Movies   int `json:"movies"`
		Episodes int `json:"episodes"`
	} `json:"added"`
	NotFound struct {
		Movies []interface{} `json:"movies"`
		Shows  []interface{} `json:"shows"`
	} `json:"not_found"`
}

func (c *APIClient) SyncHistory(req *SyncHistoryReq) (*SyncHistoryResp, error) {
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodPost,
		path:   "/sync/history",
		body:   req,
		auth:   true,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 201 {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("sync history failed (%s): %s", httpResp.Status, string(body))
	}

	var resp SyncHistoryResp
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

// SyncWatchlistReq is the POST /sync/watchlist payload. Items reuse SyncItem
// for the trakt-id shape but ignore WatchedAt (the watchlist has no watched
// timestamp — items carry a server-assigned listed_at instead).
type SyncWatchlistReq struct {
	Movies []SyncItem `json:"movies,omitempty"`
	Shows  []SyncItem `json:"shows,omitempty"`
}

// SyncWatchlistResp mirrors the Trakt /sync/watchlist response. `added` and
// `existing` are counts per type; `not_found` echoes back items the server
// couldn't match (we send by trakt id, so this should normally be empty).
type SyncWatchlistResp struct {
	Added struct {
		Movies int `json:"movies"`
		Shows  int `json:"shows"`
	} `json:"added"`
	Existing struct {
		Movies int `json:"movies"`
		Shows  int `json:"shows"`
	} `json:"existing"`
	NotFound struct {
		Movies []interface{} `json:"movies"`
		Shows  []interface{} `json:"shows"`
	} `json:"not_found"`
}

// AddWatchlist POSTs items to /sync/watchlist. Server returns 201 on success
// (even when every item is already on the list — those land under `existing`).
func (c *APIClient) AddWatchlist(req *SyncWatchlistReq) (*SyncWatchlistResp, error) {
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodPost,
		path:   "/sync/watchlist",
		body:   req,
		auth:   true,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 201 {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("add watchlist failed (%s): %s", httpResp.Status, string(body))
	}

	var resp SyncWatchlistResp
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

// RemoveWatchlistResp mirrors POST /sync/watchlist/remove. Same shape as the
// add response except `deleted` replaces `added`, and there's no `existing`
// bucket (items not on the list are silently no-ops, counted as 0 deleted).
type RemoveWatchlistResp struct {
	Deleted struct {
		Movies int `json:"movies"`
		Shows  int `json:"shows"`
	} `json:"deleted"`
	NotFound struct {
		Movies []interface{} `json:"movies"`
		Shows  []interface{} `json:"shows"`
	} `json:"not_found"`
}

// RemoveWatchlist POSTs items to /sync/watchlist/remove. Request body is
// the same shape as SyncWatchlistReq — we reuse it rather than defining a
// near-identical type.
func (c *APIClient) RemoveWatchlist(req *SyncWatchlistReq) (*RemoveWatchlistResp, error) {
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodPost,
		path:   "/sync/watchlist/remove",
		body:   req,
		auth:   true,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("remove watchlist failed (%s): %s", httpResp.Status, string(body))
	}

	var resp RemoveWatchlistResp
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

// RemoveHistoryResp mirrors POST /sync/history/remove. Trakt returns the
// deleted counts under `deleted.{movies,episodes}` (not "shows" — removing
// a show from history removes all its episodes, which get counted at the
// episode grain).
type RemoveHistoryResp struct {
	Deleted struct {
		Movies   int `json:"movies"`
		Episodes int `json:"episodes"`
	} `json:"deleted"`
	NotFound struct {
		Movies   []interface{} `json:"movies"`
		Shows    []interface{} `json:"shows"`
		Seasons  []interface{} `json:"seasons"`
		Episodes []interface{} `json:"episodes"`
		Ids      []interface{} `json:"ids"`
	} `json:"not_found"`
}

// RemoveHistory POSTs items to /sync/history/remove. Request body is the
// same shape as SyncHistoryReq (reused), but WatchedAt is ignored by the
// server on the remove path — removing by trakt id removes ALL plays of
// that item from history, not just the one at a given timestamp.
func (c *APIClient) RemoveHistory(req *SyncHistoryReq) (*RemoveHistoryResp, error) {
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodPost,
		path:   "/sync/history/remove",
		body:   req,
		auth:   true,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("remove history failed (%s): %s", httpResp.Status, string(body))
	}

	var resp RemoveHistoryResp
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

// CalendarEpisode is one entry from /calendars/my/shows. Each row is a
// single episode airing on a specific date — a show with 3 new episodes
// in the window produces 3 rows.
type CalendarEpisode struct {
	FirstAired time.Time `json:"first_aired"`
	Episode    struct {
		Season int    `json:"season"`
		Number int    `json:"number"`
		Title  string `json:"title"`
		Ids    struct {
			Trakt int    `json:"trakt"`
			Tvdb  int    `json:"tvdb"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"episode"`
	Show struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
		Ids   struct {
			Trakt int    `json:"trakt"`
			Slug  string `json:"slug"`
			Tvdb  int    `json:"tvdb"`
			Imdb  string `json:"imdb"`
			Tmdb  int    `json:"tmdb"`
		} `json:"ids"`
	} `json:"show"`
}

// GetMyShowsCalendar returns upcoming episodes of shows the user watches,
// over a window starting at `start` (YYYY-MM-DD) for `days` days. Pass an
// empty start to default to today per Trakt's server-side default. When
// `onlyNew` is true, hits /calendars/my/shows/new instead (series premieres
// only — S01E01 airings).
func (c *APIClient) GetMyShowsCalendar(start string, days int, onlyNew bool) ([]CalendarEpisode, error) {
	base := "/calendars/my/shows"
	if onlyNew {
		base = "/calendars/my/shows/new"
	}
	path := base
	// Trakt's URL shape is /calendars/my/shows[/new]/{start_date}/{days}.
	// Both path segments are optional but must be supplied together — you
	// can't specify days without start_date.
	//
	// Use LOCAL time (not UTC) for the default start date: a user in
	// UTC+12 at 1 AM local is still on yesterday's UTC date, but from
	// their perspective "today's calendar" means their local today, not
	// UTC's today. Trakt treats the date string as a calendar date, so
	// feeding it the user's local date is the right behavior.
	if start == "" {
		start = time.Now().Format("2006-01-02")
	}
	if days <= 0 {
		days = 7
	}
	path = fmt.Sprintf("%s/%s/%d", path, start, days)

	httpResp, err := c.doRequest(requestParams{
		method: http.MethodGet,
		path:   path,
		auth:   true,
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get calendar: %s", httpResp.Status)
	}

	var resp []CalendarEpisode
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *APIClient) Search(query string, searchType string) ([]SearchResult, error) {
	httpResp, err := c.doRequest(requestParams{
		method: http.MethodGet,
		path:   "/search/" + searchType,
		auth:   true,
		query: map[string]string{
			"query": query,
		},
		pagination: PaginationsParams{
			Limit: 10,
		},
	})
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("search failed: %s", httpResp.Status)
	}

	var resp []SearchResult
	err = json.NewDecoder(httpResp.Body).Decode(&resp)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
