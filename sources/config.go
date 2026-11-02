package sources

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the user's opt-in sync configuration. It lives outside the repo
// (default ~/.config/trakt-cli/sync.yaml, override with TRAKT_SYNC_CONFIG)
// because it holds personal choices: profile names, deny list, cookie command.
type Config struct {
	// Enabled is the master switch. Sync is inert unless this is true.
	Enabled bool `yaml:"enabled"`
	// CookieCommand prints cookies for a domain; "{domain}" is replaced with
	// e.g. ".netflix.com". Output may be a JSON array of {name,value}
	// objects, a "Cookie: a=b; c=d" line, or a bare "a=b; c=d" header.
	CookieCommand string `yaml:"cookie_command"`
	// Since ignores items older than this window ("30d", "72h"). Default 30d.
	Since string `yaml:"since"`
	// Deny lists show or movie titles that must never be synced (other
	// household members' viewing, kids' shows). Matched after Normalize.
	Deny []string `yaml:"deny"`
	// Allow lists titles the user approved. Shows already in the user's
	// Trakt history sync automatically; any other show, and every movie, is
	// held for review until it appears here (see `trakt-cli sync approve`).
	// This keeps someone else's viewing on a shared account out of Trakt.
	Allow []string `yaml:"allow"`
	// Overrides pins a title to a Trakt id when search is ambiguous.
	Overrides map[string]int `yaml:"overrides"`

	Sources struct {
		UpNext struct {
			Enabled bool `yaml:"enabled"`
		} `yaml:"upnext"`
		Netflix struct {
			Enabled bool   `yaml:"enabled"`
			Profile string `yaml:"profile"`
		} `yaml:"netflix"`
		HBOMax struct {
			Enabled bool   `yaml:"enabled"`
			Profile string `yaml:"profile"`
		} `yaml:"hbomax"`
		Paramount struct {
			Enabled bool `yaml:"enabled"`
			// MinProgress is the percent at which an in-progress episode
			// counts as watched. Default 80, matching Trakt's scrobble rule.
			MinProgress int `yaml:"min_progress"`
		} `yaml:"paramount"`
	} `yaml:"sources"`
}

// ConfigPath returns where the sync config is read from.
func ConfigPath() string {
	if p := os.Getenv("TRAKT_SYNC_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".trakt-sync.yaml"
	}
	return filepath.Join(home, ".config", "trakt-cli", "sync.yaml")
}

// LoadConfig reads the config. A missing file is not an error: it returns
// (nil, nil) so callers treat sync as not opted in.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Sources.Paramount.MinProgress == 0 {
		c.Sources.Paramount.MinProgress = 80
	}
	return &c, nil
}

// EnabledSources lists the source names switched on in the config.
func (c *Config) EnabledSources() []string {
	if c == nil {
		return nil
	}
	var out []string
	if c.Sources.UpNext.Enabled {
		out = append(out, "upnext")
	}
	if c.Sources.Netflix.Enabled {
		out = append(out, "netflix")
	}
	if c.Sources.HBOMax.Enabled {
		out = append(out, "hbomax")
	}
	if c.Sources.Paramount.Enabled {
		out = append(out, "paramount")
	}
	return out
}

// Active reports whether sync should run at all, and if not, why.
func (c *Config) Active() (bool, string) {
	switch {
	case c == nil:
		return false, "no sync config at " + ConfigPath()
	case !c.Enabled:
		return false, "sync config has enabled: false"
	case len(c.EnabledSources()) == 0:
		return false, "no sources enabled in sync config"
	case strings.TrimSpace(c.CookieCommand) == "":
		return false, "cookie_command is not set in sync config"
	}
	return true, ""
}

// SinceDuration parses Since ("30d", "12h", "90m"); default 30 days.
func (c *Config) SinceDuration() (time.Duration, error) {
	s := strings.TrimSpace(c.Since)
	if s == "" {
		return 30 * 24 * time.Hour, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid since %q", c.Since)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid since %q", c.Since)
	}
	return d, nil
}

// Denied reports whether a title is on the deny list.
func (c *Config) Denied(title string) bool {
	n := Normalize(title)
	if n == "" {
		return false
	}
	for _, d := range c.Deny {
		if Normalize(d) == n {
			return true
		}
	}
	return false
}

// Allowed reports whether a title was approved for syncing.
func (c *Config) Allowed(title string) bool {
	n := Normalize(title)
	if n == "" {
		return false
	}
	for _, a := range c.Allow {
		if Normalize(a) == n {
			return true
		}
	}
	return false
}

// Override returns a pinned Trakt id for a title, if configured.
func (c *Config) Override(title string) (int, bool) {
	n := Normalize(title)
	if n == "" {
		return 0, false
	}
	for k, v := range c.Overrides {
		if Normalize(k) == n {
			return v, true
		}
	}
	return 0, false
}
