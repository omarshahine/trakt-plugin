package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cliPath returns the path to the built trakt-cli binary.
func cliPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(filepath.Dir(wd), "trakt-cli")
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("trakt-cli binary not found at %s — run 'go build -o trakt-cli .' first", bin)
	}
	return bin
}

// requireAuth skips the test if ~/.trakt.yaml doesn't exist (integration tests).
func requireAuth(t *testing.T) {
	t.Helper()
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".trakt.yaml")); err != nil {
		t.Skip("~/.trakt.yaml not found — run 'trakt-cli auth' first")
	}
}

// runCLI executes trakt-cli with the given args and returns stdout.
func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	bin := cliPath(t)
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trakt-cli %s failed: %v\nOutput: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

// parseJSON unmarshals JSON output into a map.
func parseJSON(t *testing.T, data string) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v\nData: %s", err, data)
	}
	return result
}

// ============================================================
// Unit tests — run in CI, no auth or network required
// ============================================================

func TestHelp(t *testing.T) {
	out := runCLI(t, "--help")
	if !strings.Contains(out, "trakt-cli") {
		t.Error("--help should mention trakt-cli")
	}
	for _, cmd := range []string{"auth", "search", "history", "watchlist", "progress"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("--help should list %q command", cmd)
		}
	}
}

func TestAuthHelp(t *testing.T) {
	out := runCLI(t, "auth", "--help")
	if !strings.Contains(out, "device code flow") {
		t.Error("auth --help should mention device code flow")
	}
	if strings.Contains(out, "required") {
		t.Error("auth flags should be optional (built-in defaults)")
	}
}

func TestJsonFlag(t *testing.T) {
	out := runCLI(t, "--help")
	if !strings.Contains(out, "--json") {
		t.Error("--help should show --json global flag")
	}
}

func TestSubcommandHelp(t *testing.T) {
	cmds := []struct {
		args     []string
		contains string
	}{
		{[]string{"search", "--help"}, "Search for movies"},
		{[]string{"history", "--help"}, "watched history"},
		{[]string{"watchlist", "--help"}, "watchlist"},
		{[]string{"progress", "--help"}, "in progress"},
		{[]string{"history", "add", "--help"}, "watch history"},
	}
	for _, tc := range cmds {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			out := runCLI(t, tc.args...)
			if !strings.Contains(out, tc.contains) {
				t.Errorf("%v help should contain %q", tc.args, tc.contains)
			}
		})
	}
}

func TestNoAuthClean(t *testing.T) {
	bin := cliPath(t)
	cmd := exec.Command(bin, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--help failed: %v", err)
	}
	output := string(out)
	if strings.Contains(output, "Failed to read") {
		t.Error("--help should not show 'Failed to read' error")
	}
	if strings.Contains(output, "level=error") {
		t.Error("--help should not show error-level log messages")
	}
}

func TestSourceURL(t *testing.T) {
	out := runCLI(t, "--help")
	if !strings.Contains(out, "omarshahine/trakt-plugin") {
		t.Error("source URL should point to omarshahine/trakt-plugin")
	}
}

// ============================================================
// Integration tests — require auth, hit live Trakt API
// Run with: go test ./cmd/ -v -tags integration
// Skipped in CI (no ~/.trakt.yaml)
// ============================================================

func TestSearchJSON(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "search", "Inception", "--type", "movie", "--json")
	result := parseJSON(t, out)

	items, ok := result["items"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatal("search should return non-empty items array")
	}

	first := items[0].(map[string]interface{})
	if first["type"] != "movie" {
		t.Errorf("expected type=movie, got %v", first["type"])
	}
	if first["title"] != "Inception" {
		t.Errorf("expected first result title=Inception, got %v", first["title"])
	}
	if _, hasScore := first["score"]; hasScore {
		t.Error("search results should not include score field")
	}
	if first["imdb"] != "tt1375666" {
		t.Errorf("expected imdb=tt1375666, got %v", first["imdb"])
	}
}

func TestSearchShowType(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "search", "Severance", "--type", "show", "--json")
	result := parseJSON(t, out)

	items := result["items"].([]interface{})
	for _, item := range items {
		m := item.(map[string]interface{})
		if m["type"] != "show" {
			t.Errorf("with --type show, got type=%v", m["type"])
		}
	}
}

func TestSearchNoResults(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "search", "xyznonexistentmovie12345", "--json")
	result := parseJSON(t, out)

	items := result["items"].([]interface{})
	if len(items) != 0 {
		t.Errorf("expected empty results for nonsense query, got %d", len(items))
	}
}

func TestHistoryJSON(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "history", "--limit", "3", "--json")
	result := parseJSON(t, out)

	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatal("history should return items array")
	}
	if len(items) > 3 {
		t.Errorf("--limit 3 should return at most 3 items, got %d", len(items))
	}
	if _, ok := result["page"]; !ok {
		t.Error("history should include page field")
	}
	if _, ok := result["item_count"]; !ok {
		t.Error("history should include item_count field")
	}
}

func TestHistoryTypeFilter(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "history", "--type", "movies", "--limit", "3", "--json")
	result := parseJSON(t, out)

	items := result["items"].([]interface{})
	for _, item := range items {
		m := item.(map[string]interface{})
		if m["type"] != "movie" {
			t.Errorf("with --type movies, got type=%v", m["type"])
		}
	}
}

func TestHistoryEpisodeFields(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "history", "--type", "shows", "--limit", "5", "--json")
	result := parseJSON(t, out)

	items := result["items"].([]interface{})
	if len(items) == 0 {
		t.Skip("no show history found")
	}

	first := items[0].(map[string]interface{})
	if first["type"] != "episode" {
		t.Skipf("first item is not an episode: %v", first["type"])
	}
	for _, field := range []string{"title", "season", "episode", "show_title", "watched_at"} {
		if _, ok := first[field]; !ok {
			t.Errorf("episode should have %q field", field)
		}
	}
}

func TestWatchlistJSON(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "watchlist", "--limit", "5", "--json")
	result := parseJSON(t, out)

	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatal("watchlist should return items array")
	}
	if len(items) == 0 {
		t.Skip("watchlist is empty")
	}

	first := items[0].(map[string]interface{})
	for _, field := range []string{"type", "title", "year", "trakt_id", "added_at"} {
		if _, ok := first[field]; !ok {
			t.Errorf("watchlist item should have %q field", field)
		}
	}
}

func TestWatchlistTypeFilter(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "watchlist", "--type", "movies", "--limit", "5", "--json")
	result := parseJSON(t, out)

	items := result["items"].([]interface{})
	for _, item := range items {
		m := item.(map[string]interface{})
		if m["type"] != "movie" {
			t.Errorf("with --type movies, got type=%v", m["type"])
		}
	}
}

func TestProgressJSON(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "progress", "--json")
	result := parseJSON(t, out)

	if _, ok := result["in_progress"]; !ok {
		t.Error("progress should include in_progress field")
	}
	if _, ok := result["summary"]; !ok {
		t.Error("progress (default) should include summary field")
	}

	inProgress, ok := result["in_progress"].([]interface{})
	if !ok || len(inProgress) == 0 {
		t.Skip("no in-progress shows")
	}

	first := inProgress[0].(map[string]interface{})
	for _, field := range []string{"title", "year", "trakt_id", "aired", "watched", "remaining", "percent", "status"} {
		if _, ok := first[field]; !ok {
			t.Errorf("progress item should have %q field", field)
		}
	}
}

func TestProgressAll(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "progress", "--all", "--json")
	result := parseJSON(t, out)

	for _, key := range []string{"in_progress", "not_started", "completed"} {
		if _, ok := result[key]; !ok {
			t.Errorf("progress --all should include %q field", key)
		}
	}
	if _, ok := result["summary"]; ok {
		t.Error("progress --all should not include summary field")
	}
}

func TestSearchTable(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "search", "Inception", "--type", "movie")
	if !strings.Contains(out, "Inception") {
		t.Error("table output should contain Inception")
	}
	if !strings.Contains(out, "Movie") {
		t.Error("table output should contain type column")
	}
}

func TestHistoryTable(t *testing.T) {
	requireAuth(t)
	out := runCLI(t, "history", "--limit", "3")
	if !strings.Contains(out, "Page") {
		t.Error("table output should show page info")
	}
}
