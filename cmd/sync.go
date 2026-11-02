package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/omarshahine/trakt-plugin/api"
	"github.com/omarshahine/trakt-plugin/sources"
	"github.com/spf13/cobra"
)

// defaultMaxNewShowEpisodes guards against a single pointer importing a whole
// back catalog for an approved show the user has never logged. Override with
// --max-new-show-episodes.
const defaultMaxNewShowEpisodes = 25

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Import watched items from streaming services (opt-in)",
	Long: `Import what you watched on streaming services into your Trakt history.

Opt-in: does nothing until you create ` + "`~/.config/trakt-cli/sync.yaml`" + ` (or set
TRAKT_SYNC_CONFIG) with "enabled: true", at least one source, and a
cookie_command that prints your browser cookies for a domain.

Sources: upnext (Apple TV app Up Next via tv.apple.com), netflix, hbomax,
paramount. Items on the deny list, older than "since", or already in your
history are skipped. Shows you already have plays for sync automatically;
any other show, and every movie, is held for review until you approve it
with ` + "`trakt-cli sync approve <title>`" + `. Use --dry-run to preview.`,
	RunE:         runSync,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.PersistentFlags().String("config", "", "Path to sync config (default ~/.config/trakt-cli/sync.yaml)")
	syncCmd.Flags().Bool("dry-run", false, "Show what would be added without writing to Trakt")
	syncCmd.Flags().StringSlice("source", nil, "Only run these sources (default: all enabled in config)")
	syncCmd.Flags().Int("max-new-show-episodes", defaultMaxNewShowEpisodes, "Skip an approved never-logged show if more than this many episodes would be added")
}

type syncSourceReport struct {
	Items int    `json:"items"`
	Error string `json:"error,omitempty"`
}

type syncPlanned struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	TraktID   int    `json:"trakt_id"`
	Season    int    `json:"season,omitempty"`
	Episode   int    `json:"episode,omitempty"`
	WatchedAt string `json:"watched_at"`
	Source    string `json:"source"`
	Inferred  bool   `json:"inferred,omitempty"`
	// NeedsReview: evidence too weak to write without approval.
	NeedsReview bool `json:"needs_review,omitempty"`
}

type syncPending struct {
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Items    int      `json:"items"`
	LastSeen string   `json:"last_seen"`
	Sources  []string `json:"sources"`
	Episodes []string `json:"episodes,omitempty"`
}

type syncReport struct {
	Enabled        bool                        `json:"enabled"`
	Reason         string                      `json:"reason,omitempty"`
	ConfigPath     string                      `json:"config_path"`
	DryRun         bool                        `json:"dry_run"`
	Sources        map[string]syncSourceReport `json:"sources,omitempty"`
	Planned        []syncPlanned               `json:"planned"`
	SkippedDenied  int                         `json:"skipped_denied"`
	SkippedOld     int                         `json:"skipped_old"`
	AlreadyWatched int                         `json:"already_watched"`
	Unresolved     []string                    `json:"unresolved,omitempty"`
	Capped         []string                    `json:"capped,omitempty"`
	// PendingReview holds titles not yet in Trakt history and not approved:
	// nothing is written for them until `trakt-cli sync approve <title>`.
	PendingReview []syncPending `json:"pending_review"`
	Added         *struct {
		Movies   int `json:"movies"`
		Episodes int `json:"episodes"`
	} `json:"added,omitempty"`
	NotFound int `json:"not_found,omitempty"`
}

func runSync(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("config")
	explicit := path != ""
	if path == "" {
		path = sources.ConfigPath()
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	only, _ := cmd.Flags().GetStringSlice("source")
	maxNew, _ := cmd.Flags().GetInt("max-new-show-episodes")

	rep := &syncReport{ConfigPath: path, DryRun: dryRun, Planned: []syncPlanned{}, PendingReview: []syncPending{}}
	cfg, err := sources.LoadConfig(path)
	if err != nil {
		return err
	}
	if explicit && cfg == nil {
		return fmt.Errorf("no sync config at %s", path)
	}
	if ok, why := cfg.Active(); !ok {
		rep.Reason = why
		return emitSync(rep, "Sync is off: "+why+".\nSee `trakt-cli sync --help` to opt in.")
	}
	rep.Enabled = true
	since, err := cfg.SinceDuration()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	now := time.Now()
	cookies := sources.CommandCookieLoader(cfg.CookieCommand)
	want := map[string]bool{}
	for _, s := range cfg.EnabledSources() {
		if len(only) == 0 || contains(only, s) {
			want[s] = true
		}
	}

	// Collect. Up Next runs first so HBO Max can check the shows it saw.
	rep.Sources = map[string]syncSourceReport{}
	var watches []sources.Watch
	run := func(s sources.Source) {
		got, err := s.Fetch(ctx)
		r := syncSourceReport{Items: len(got)}
		if err != nil {
			r.Error = err.Error()
		}
		rep.Sources[s.Name()] = r
		watches = append(watches, got...)
	}
	upnext := &sources.UpNext{Cookies: cookies}
	if want["upnext"] {
		run(upnext)
	}
	if want["netflix"] {
		run(&sources.Netflix{Cookies: cookies, Profile: cfg.Sources.Netflix.Profile})
	}
	if want["hbomax"] {
		run(&sources.HBOMax{Cookies: cookies, Profile: cfg.Sources.HBOMax.Profile, Candidates: upnext.Unlabeled})
	}
	if want["paramount"] {
		run(&sources.Paramount{Cookies: cookies, MinProgress: cfg.Sources.Paramount.MinProgress})
	}

	// Filter: deny list and age window. Unknown times become "now".
	var kept []sources.Watch
	for _, w := range watches {
		if cfg.Denied(w.Title()) {
			rep.SkippedDenied++
			continue
		}
		if w.WatchedAt.IsZero() {
			w.WatchedAt = now
		}
		if since > 0 && now.Sub(w.WatchedAt) > since {
			rep.SkippedOld++
			continue
		}
		kept = append(kept, w)
	}

	client := api.NewAPIClient()
	res := newResolver(&client, cfg)
	defer res.save()

	type epKey struct{ show, season, episode int }
	episodes := map[epKey]syncPlanned{}
	movies := map[int]syncPlanned{}
	showTitles := map[int]string{}
	unresolved := map[string]bool{}

	for _, w := range kept {
		switch w.Kind {
		case sources.KindMovie:
			id, title, ok := res.movie(w.MovieTitle, w.Year)
			if !ok {
				unresolved["movie: "+w.MovieTitle] = true
				continue
			}
			p := syncPlanned{Kind: "movie", Title: title, TraktID: id, WatchedAt: w.WatchedAt.UTC().Format(time.RFC3339), Source: w.Source}
			if prev, dup := movies[id]; !dup || p.WatchedAt < prev.WatchedAt {
				movies[id] = p
			}
		case sources.KindEpisode:
			id, title, ok := res.show(w.ShowTitle, w.Year)
			if !ok {
				unresolved["show: "+w.ShowTitle] = true
				continue
			}
			ep := w.Episode
			if ep == 0 {
				ep = res.episodeByTitle(id, w.Season, w.EpisodeTitle)
				if ep == 0 {
					unresolved[fmt.Sprintf("episode: %s S%d %q", w.ShowTitle, w.Season, w.EpisodeTitle)] = true
					continue
				}
			}
			showTitles[id] = title
			k := epKey{id, w.Season, ep}
			p := syncPlanned{Kind: "episode", Title: title, TraktID: id, Season: w.Season, Episode: ep,
				WatchedAt: w.WatchedAt.UTC().Format(time.RFC3339), Source: w.Source, Inferred: w.Inferred, NeedsReview: w.NeedsReview}
			// Prefer stronger evidence (confirmed over needs-review, direct over
			// inferred), then the earliest time.
			if prev, dup := episodes[k]; !dup || evidenceRank(p) > evidenceRank(prev) || (evidenceRank(p) == evidenceRank(prev) && p.WatchedAt < prev.WatchedAt) {
				episodes[k] = p
			}
		}
	}

	// Dedupe against Trakt history. Shows never logged need approval.
	byShow := map[int][]syncPlanned{}
	for _, p := range episodes {
		byShow[p.TraktID] = append(byShow[p.TraktID], p)
	}
	for id, eps := range byShow {
		watched, err := client.WatchedEpisodeSet(id)
		if err != nil {
			unresolved[fmt.Sprintf("history lookup failed for %s", showTitles[id])] = true
			continue
		}
		var fresh []syncPlanned
		for _, p := range eps {
			if watched[fmt.Sprintf("%d:%d", p.Season, p.Episode)] {
				rep.AlreadyWatched++
				continue
			}
			fresh = append(fresh, p)
		}
		if len(watched) == 0 && !cfg.Allowed(showTitles[id]) {
			if len(fresh) > 0 {
				rep.PendingReview = append(rep.PendingReview, pendingFrom("show", showTitles[id], fresh))
			}
			continue
		}
		// Known show: weak evidence (e.g. episodes implied by an Up Next
		// pointer) still needs approval, so a skipped episode or someone
		// else's viewing is never written automatically.
		if !cfg.Allowed(showTitles[id]) {
			var auto, weak []syncPlanned
			for _, p := range fresh {
				if p.NeedsReview {
					weak = append(weak, p)
				} else {
					auto = append(auto, p)
				}
			}
			if len(weak) > 0 {
				rep.PendingReview = append(rep.PendingReview, pendingFrom("show", showTitles[id], weak))
			}
			fresh = auto
		}
		if len(watched) == 0 && maxNew > 0 && len(fresh) > maxNew {
			rep.Capped = append(rep.Capped, fmt.Sprintf("%s (%d episodes; raise --max-new-show-episodes to import)", showTitles[id], len(fresh)))
			continue
		}
		rep.Planned = append(rep.Planned, fresh...)
	}
	if len(movies) > 0 {
		seen, err := watchedMovieIDs(&client)
		if err != nil {
			return fmt.Errorf("read movie history: %w", err)
		}
		for id, p := range movies {
			if seen[id] {
				rep.AlreadyWatched++
				continue
			}
			if !cfg.Allowed(p.Title) {
				rep.PendingReview = append(rep.PendingReview, pendingFrom("movie", p.Title, []syncPlanned{p}))
				continue
			}
			rep.Planned = append(rep.Planned, p)
		}
	}
	for u := range unresolved {
		rep.Unresolved = append(rep.Unresolved, u)
	}
	sort.Strings(rep.Unresolved)
	sort.Slice(rep.Planned, func(i, j int) bool {
		a, b := rep.Planned[i], rep.Planned[j]
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		if a.Season != b.Season {
			return a.Season < b.Season
		}
		return a.Episode < b.Episode
	})
	sort.Strings(rep.Capped)
	sort.Slice(rep.PendingReview, func(i, j int) bool { return rep.PendingReview[i].LastSeen > rep.PendingReview[j].LastSeen })

	if !dryRun && len(rep.Planned) > 0 {
		resp, err := client.SyncHistory(buildSyncReq(rep.Planned))
		if err != nil {
			return err
		}
		rep.Added = &struct {
			Movies   int `json:"movies"`
			Episodes int `json:"episodes"`
		}{resp.Added.Movies, resp.Added.Episodes}
		rep.NotFound = len(resp.NotFound.Movies) + len(resp.NotFound.Shows) + len(resp.NotFound.Seasons) + len(resp.NotFound.Episodes)
	}

	if err := emitSync(rep, ""); err != nil {
		return err
	}
	for _, r := range rep.Sources {
		if r.Error != "" {
			return fmt.Errorf("one or more sources failed")
		}
	}
	return nil
}

func evidenceRank(p syncPlanned) int {
	switch {
	case p.NeedsReview:
		return 0
	case p.Inferred:
		return 1
	}
	return 2
}

func pendingFrom(kind, title string, items []syncPlanned) syncPending {
	p := syncPending{Kind: kind, Title: title, Items: len(items)}
	srcs := map[string]bool{}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Season != items[j].Season {
			return items[i].Season < items[j].Season
		}
		return items[i].Episode < items[j].Episode
	})
	for _, it := range items {
		srcs[it.Source] = true
		if it.WatchedAt > p.LastSeen {
			p.LastSeen = it.WatchedAt
		}
		if kind == "show" && len(p.Episodes) < 12 {
			p.Episodes = append(p.Episodes, fmt.Sprintf("S%02dE%02d", it.Season, it.Episode))
		}
	}
	if len(p.LastSeen) >= 10 {
		p.LastSeen = p.LastSeen[:10]
	}
	for s := range srcs {
		p.Sources = append(p.Sources, s)
	}
	sort.Strings(p.Sources)
	return p
}

func buildSyncReq(planned []syncPlanned) *api.SyncHistoryReq {
	req := &api.SyncHistoryReq{}
	shows := map[int]map[int][]api.SyncEpisode{}
	var order []int
	for _, p := range planned {
		if p.Kind == "movie" {
			item := api.SyncItem{WatchedAt: p.WatchedAt}
			item.Ids.Trakt = p.TraktID
			req.Movies = append(req.Movies, item)
			continue
		}
		if shows[p.TraktID] == nil {
			shows[p.TraktID] = map[int][]api.SyncEpisode{}
			order = append(order, p.TraktID)
		}
		shows[p.TraktID][p.Season] = append(shows[p.TraktID][p.Season], api.SyncEpisode{Number: p.Episode, WatchedAt: p.WatchedAt})
	}
	for _, id := range order {
		item := api.SyncItem{}
		item.Ids.Trakt = id
		var seasons []int
		for s := range shows[id] {
			seasons = append(seasons, s)
		}
		sort.Ints(seasons)
		for _, s := range seasons {
			item.Seasons = append(item.Seasons, api.SyncSeason{Number: s, Episodes: shows[id][s]})
		}
		req.Shows = append(req.Shows, item)
	}
	return req
}

func watchedMovieIDs(c *api.APIClient) (map[int]bool, error) {
	slug, err := c.GetUserSlug()
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	// Read to the last (short) page. A partial set would let a duplicate
	// play through, so hitting the safety ceiling is an error, not success.
	const maxPages = 5000
	for page := 1; page <= maxPages; page++ {
		h, _, err := c.GetUserHistory(slug, "movies", api.PaginationsParams{Page: page, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, it := range h {
			seen[it.Movie.Ids.Trakt] = true
		}
		if len(h) < 100 {
			return seen, nil
		}
	}
	return nil, fmt.Errorf("movie history exceeds %d pages; refusing to dedupe against a partial set", maxPages)
}

func emitSync(rep *syncReport, message string) error {
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	if message != "" {
		fmt.Println(message)
		return nil
	}
	names := make([]string, 0, len(rep.Sources))
	for n := range rep.Sources {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r := rep.Sources[n]
		if r.Error != "" {
			fmt.Printf("  %-10s error: %s\n", n, r.Error)
		} else {
			fmt.Printf("  %-10s %d items\n", n, r.Items)
		}
	}
	fmt.Printf("Skipped: %d denied, %d older than window, %d already in history\n", rep.SkippedDenied, rep.SkippedOld, rep.AlreadyWatched)
	for _, u := range rep.Unresolved {
		fmt.Println("  unresolved:", u)
	}
	for _, c := range rep.Capped {
		fmt.Println("  capped:", c)
	}
	if len(rep.PendingReview) > 0 {
		fmt.Println("Needs your review (not in your Trakt history yet; nothing written):")
		for _, p := range rep.PendingReview {
			detail := ""
			if len(p.Episodes) > 0 {
				detail = " " + strings.Join(p.Episodes, " ")
			}
			fmt.Printf("  %-6s %s (%d, last %s, via %s)%s\n", p.Kind, p.Title, p.Items, p.LastSeen, strings.Join(p.Sources, ","), detail)
		}
		fmt.Println(`Approve with: trakt-cli sync approve "<title>"   Reject with: trakt-cli sync deny "<title>"`)
	}
	if len(rep.Planned) == 0 {
		fmt.Println("Nothing new to add.")
		return nil
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Title", "Item", "Watched", "Source"})
	for _, p := range rep.Planned {
		item := "movie"
		if p.Kind == "episode" {
			item = fmt.Sprintf("S%02dE%02d", p.Season, p.Episode)
			if p.Inferred {
				item += " *"
			}
		}
		t.AppendRow(table.Row{p.Title, item, p.WatchedAt[:10], p.Source})
	}
	t.SetStyle(table.StyleRounded)
	t.Render()
	switch {
	case rep.DryRun:
		fmt.Printf("Dry run: %d items would be added (* = inferred from Up Next).\n", len(rep.Planned))
	case rep.Added != nil:
		fmt.Printf("Added %d episodes and %d movies.\n", rep.Added.Episodes, rep.Added.Movies)
		if rep.NotFound > 0 {
			fmt.Printf("Trakt did not recognize %d items.\n", rep.NotFound)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), s) {
			return true
		}
	}
	return false
}

// resolver maps service titles to Trakt ids, caching hits on disk.
type resolver struct {
	client *api.APIClient
	cfg    *sources.Config
	path   string
	cache  map[string]resolved
	dirty  bool
	eps    map[int][]api.SeasonEpisodes
	// owned holds Trakt ids the user already has plays for, per kind,
	// loaded lazily to break same-title ties.
	owned map[string]map[int]bool
}

type resolved struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

func newResolver(c *api.APIClient, cfg *sources.Config) *resolver {
	r := &resolver{client: c, cfg: cfg, cache: map[string]resolved{}, eps: map[int][]api.SeasonEpisodes{}}
	if dir, err := os.UserCacheDir(); err == nil {
		r.path = filepath.Join(dir, "trakt-cli", "sync-map-v2.json") // v2: ambiguity-aware; v1 entries may be first-match guesses
		if b, err := os.ReadFile(r.path); err == nil {
			_ = json.Unmarshal(b, &r.cache)
		}
	}
	return r
}

func (r *resolver) save() {
	if !r.dirty || r.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(r.path), 0o755)
	if b, err := json.MarshalIndent(r.cache, "", "  "); err == nil {
		_ = os.WriteFile(r.path, b, 0o644)
	}
}

func (r *resolver) show(title string, year int) (int, string, bool) {
	return r.lookup("show", title, year)
}

func (r *resolver) movie(title string, year int) (int, string, bool) {
	return r.lookup("movie", title, year)
}

// lookup requires an exact normalized title match (and year when known);
// anything fuzzier is left unresolved rather than risk a wrong show.
func (r *resolver) lookup(kind, title string, year int) (int, string, bool) {
	if id, ok := r.cfg.Override(title); ok {
		return id, title, true
	}
	key := fmt.Sprintf("%s:%s:%d", kind, sources.Normalize(title), year)
	if hit, ok := r.cache[key]; ok {
		return hit.ID, hit.Title, hit.ID != 0
	}
	results, err := r.client.Search(title, kind)
	if err != nil {
		return 0, "", false
	}
	want := sources.Normalize(title)
	if want == "" {
		return 0, "", false
	}
	// Collect every exact match. Remakes and reboots share titles, and most
	// sources report no year. With several distinct ids, prefer the single
	// one the user already has plays for; otherwise leave it unresolved (pin
	// it with overrides) rather than guess.
	var cands []resolved
	seenID := map[int]bool{}
	for _, s := range results {
		var t string
		var y, id int
		switch {
		case kind == "show" && s.Show != nil:
			t, y, id = s.Show.Title, s.Show.Year, s.Show.Ids.Trakt
		case kind == "movie" && s.Movie != nil:
			t, y, id = s.Movie.Title, s.Movie.Year, s.Movie.Ids.Trakt
		default:
			continue
		}
		if sources.Normalize(t) != want || (year != 0 && y != year) || seenID[id] {
			continue
		}
		seenID[id] = true
		cands = append(cands, resolved{ID: id, Title: t})
	}
	var match *resolved
	switch {
	case len(cands) == 1:
		match = &cands[0]
	case len(cands) > 1:
		owned := r.ownedIDs(kind)
		var mine []resolved
		for _, c := range cands {
			if owned[c.ID] {
				mine = append(mine, c)
			}
		}
		if len(mine) == 1 {
			match = &mine[0]
		}
	}
	if match == nil {
		return 0, "", false
	}
	r.cache[key] = *match
	r.dirty = true
	return match.ID, match.Title, true
}

// ownedIDs returns the Trakt ids the user already has plays for. A failed
// lookup yields an empty set, which keeps ambiguous titles unresolved.
func (r *resolver) ownedIDs(kind string) map[int]bool {
	if r.owned == nil {
		r.owned = map[string]map[int]bool{}
	}
	if ids, ok := r.owned[kind]; ok {
		return ids
	}
	ids := map[int]bool{}
	switch kind {
	case "show":
		if slug, err := r.client.GetUserSlug(); err == nil {
			if watched, err := r.client.GetUserWatched(slug, "shows"); err == nil {
				for _, w := range watched {
					if w.Show != nil {
						ids[w.Show.Ids.Trakt] = true
					}
				}
			}
		}
	case "movie":
		if seen, err := watchedMovieIDs(r.client); err == nil {
			ids = seen
		}
	}
	r.owned[kind] = ids
	return ids
}

func (r *resolver) episodeByTitle(showID, season int, title string) int {
	if title == "" {
		return 0
	}
	seasons, ok := r.eps[showID]
	if !ok {
		var err error
		seasons, err = r.client.GetSeasonEpisodes(showID)
		if err != nil {
			return 0
		}
		r.eps[showID] = seasons
	}
	want := sources.Normalize(title)
	for _, s := range seasons {
		if s.Number != season {
			continue
		}
		for _, e := range s.Episodes {
			if sources.Normalize(e.Title) == want {
				return e.Number
			}
		}
	}
	return 0
}
