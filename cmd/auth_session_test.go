package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
	"github.com/spf13/cobra"
)

type authFixtureTransport func(*http.Request) (*http.Response, error)

func (fn authFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }
func authFixtureResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func authFixture(t *testing.T, respond authFixtureTransport) (*api.APIClient, *time.Time) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("machine auth requires validated private session storage; Windows uses plain interactive auth")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	clock := time.Now().UTC().Truncate(time.Second)
	old := authNow
	authNow = func() time.Time { return clock }
	t.Cleanup(func() { authNow = old })
	client := api.NewAPIClientWithCredentials(api.Credentials{})
	client.Client = &http.Client{Transport: respond}
	return &client, &clock
}

const authDeviceFixture = `{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://trakt.tv/activate","expires_in":600,"interval":5}`
const authTokenFixture = `{"access_token":"SYNTHETIC-ACCESS","refresh_token":"SYNTHETIC-REFRESH"}`

func assertPublicAuth(t *testing.T, value authOutput) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"SYNTHETIC-PRIVATE-DEVICE", "SYNTHETIC-SECRET", "SYNTHETIC-ACCESS", "SYNTHETIC-REFRESH"} {
		if bytes.Contains(b, []byte(s)) {
			t.Fatal("private credential entered public output")
		}
	}
}

func TestAuthStartReturnsInstructionsBeforeApproval(t *testing.T) {
	calls := 0
	client, _ := authFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/oauth/device/code" {
			t.Fatal("start must not poll")
		}
		return authFixtureResponse(200, authDeviceFixture), nil
	})
	output, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || output.Status != "pending" || output.UserCode != "SYNTHETIC-CODE" || !authSessionPattern.MatchString(output.Session) {
		t.Fatalf("unexpected public result: %+v calls=%d", output, calls)
	}
	assertPublicAuth(t, output)
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".trakt.yaml")); !os.IsNotExist(err) {
		t.Fatal("start must not create account credentials")
	}
	dir, err := authDirectory()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, output.Session+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("session mode=%v", info.Mode())
	}
	state, err := readAuthState(filepath.Join(dir, output.Session+".json"))
	if err != nil || state.ClientID != "SYNTHETIC-ID" || state.ClientSecret != "SYNTHETIC-SECRET" {
		t.Fatal("session not bound to initial client")
	}
}

func TestAuthPollIsOneAttemptAndEnforcesInterval(t *testing.T) {
	polls := 0
	client, clock := authFixture(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/oauth/device/code" {
			return authFixtureResponse(200, authDeviceFixture), nil
		}
		if req.URL.Path != "/oauth/device/token" {
			t.Fatal("unexpected account request")
		}
		polls++
		if polls == 1 {
			return authFixtureResponse(400, `{}`), nil
		}
		return authFixtureResponse(200, authTokenFixture), nil
	})
	started, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	early, err := pollAuthSession(client, started.Session)
	if err != nil || early.Status != "pending" || polls != 0 {
		t.Fatal("early poll must not contact provider")
	}
	*clock = clock.Add(5 * time.Second)
	pending, err := pollAuthSession(client, started.Session)
	if err != nil || pending.Status != "pending" || polls != 1 {
		t.Fatal("pending poll must make exactly one attempt")
	}
	assertPublicAuth(t, pending)
	*clock = clock.Add(5 * time.Second)
	complete, err := pollAuthSession(client, started.Session)
	if err != nil || complete.Status != "authenticated" || polls != 2 {
		t.Fatalf("complete=%+v err=%v polls=%d", complete, err, polls)
	}
	assertPublicAuth(t, complete)
	dir, _ := authDirectory()
	if _, err := os.Stat(filepath.Join(dir, started.Session+".json")); !os.IsNotExist(err) {
		t.Fatal("completed private state remains")
	}
	creds, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".trakt.yaml"))
	if err != nil || !bytes.Contains(creds, []byte("SYNTHETIC-REFRESH")) {
		t.Fatal("credentials not durably saved")
	}
}

func TestAuthPersistenceFailureCanRetryWithoutReusingDeviceCode(t *testing.T) {
	polls := 0
	client, clock := authFixture(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/oauth/device/code" {
			return authFixtureResponse(200, authDeviceFixture), nil
		}
		polls++
		return authFixtureResponse(200, authTokenFixture), nil
	})
	started, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(os.Getenv("HOME"), ".trakt.yaml")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(5 * time.Second)
	if _, err := pollAuthSession(client, started.Session); err == nil {
		t.Fatal("must not claim success after failed credential write")
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	done, err := pollAuthSession(client, started.Session)
	if err != nil || done.Status != "authenticated" || polls != 1 {
		t.Fatalf("private grant retry failed: status=%s polls=%d err=%v", done.Status, polls, err)
	}
}

func TestAuthSessionsExpireAndAreBounded(t *testing.T) {
	calls := 0
	client, clock := authFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/oauth/device/code" {
			t.Fatal("expired session must not poll")
		}
		return authFixtureResponse(200, authDeviceFixture), nil
	})
	var first authOutput
	var second authOutput
	for i := 0; i < maxAuthSessions; i++ {
		out, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = out
		}
		if i == 1 {
			second = out
		}
	}
	if _, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET"); err == nil || calls != maxAuthSessions {
		t.Fatal("session cap must precede provider call")
	}
	dir, _ := authDirectory()
	staleLock := filepath.Join(dir, second.Session+".json.lock")
	if err := os.WriteFile(staleLock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	past := clock.Add(-10 * time.Minute)
	if err := os.Chtimes(staleLock, past, past); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(601 * time.Second)
	if _, err := pollAuthSession(client, first.Session); err == nil {
		t.Fatal("expired session accepted")
	}
	if _, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET"); err != nil {
		t.Fatalf("expired states not reclaimed: %v", err)
	}
}

func TestAuthSessionRejectsTraversalLinksAndOversize(t *testing.T) {
	client, _ := authFixture(t, func(req *http.Request) (*http.Response, error) {
		t.Fatal("invalid session contacted provider")
		return nil, nil
	})
	if _, err := pollAuthSession(client, "../synthetic"); err == nil {
		t.Fatal("traversal accepted")
	}
	dir, err := authDirectory()
	if err != nil {
		t.Fatal(err)
	}
	handle := strings.Repeat("a", 32)
	path := filepath.Join(dir, handle+".json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxAuthStateBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := pollAuthSession(client, handle); err == nil {
		t.Fatal("oversize session accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "synthetic-state")
	if err := os.WriteFile(other, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := pollAuthSession(client, handle); err == nil {
		t.Fatal("session symlink accepted")
	}
	b, err := os.ReadFile(other)
	if err != nil || string(b) != "unchanged" {
		t.Fatal("symlink target changed")
	}
}

func TestAuthRejectsSymlinkedStateDirectory(t *testing.T) {
	_, _ = authFixture(t, func(req *http.Request) (*http.Response, error) { t.Fatal("must not contact provider"); return nil, nil })
	if err := os.Symlink(t.TempDir(), filepath.Join(os.Getenv("HOME"), ".config")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := authDirectory(); err == nil {
		t.Fatal("symlinked directory accepted")
	}
}

func authFixtureCommand(jsonOut bool) *cobra.Command {
	command := &cobra.Command{RunE: runAuth}
	command.SetArgs([]string{})
	command.Flags().Bool("json", jsonOut, "")
	command.Flags().String("session", "", "")
	command.Flags().String("client-id", "SYNTHETIC-ID", "")
	command.Flags().String("client-secret", "SYNTHETIC-SECRET", "")
	return command
}

func TestAuthCommandPublicJSONAndInteractiveWriteFailure(t *testing.T) {
	client, _ := authFixture(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/oauth/device/code" {
			return authFixtureResponse(200, authDeviceFixture), nil
		}
		return authFixtureResponse(200, authTokenFixture), nil
	})
	old := authClientFactory
	authClientFactory = func() api.APIClient { return *client }
	t.Cleanup(func() { authClientFactory = old })
	command := authFixtureCommand(true)
	var out bytes.Buffer
	command.SetOut(&out)
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var decoded authOutput
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	assertPublicAuth(t, decoded)
	if decoded.UserCode == "" {
		t.Fatal("instructions not returned")
	}
	if err := os.Mkdir(filepath.Join(os.Getenv("HOME"), ".trakt.yaml"), 0700); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	command = authFixtureCommand(false)
	command.SetOut(&out)
	if err := command.ExecuteContext(context.Background()); err == nil {
		t.Fatal("interactive auth must return write error")
	}
	if strings.Contains(out.String(), "Successfully authenticated") {
		t.Fatal("false success after write failure")
	}
}

func TestAuthStartRejectsUnsafeOrSecretReflectingInstructions(t *testing.T) {
	for _, body := range []string{
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"http://trakt.tv/activate","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://example.invalid/activate","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-SECRET","verification_url":"https://trakt.tv/activate","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-PRIVATE-DEVICE","verification_url":"https://trakt.tv/activate","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://trakt.tv/activate","expires_in":600,"interval":0}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://trakt.tv/activate?x=SYNTHETIC%2DSECRET","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://trakt.tv/activate#SYNTHETIC%2DSECRET","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://trakt.tv/activate/SYNTHETIC%2DSECRET","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC\u001bCODE","verification_url":"https://trakt.tv/activate","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC%2DCODE","verification_url":"https://trakt.tv/activate","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://trakt.tv/activate?","expires_in":600,"interval":5}`,
		`{"device_code":"SYNTHETIC-PRIVATE-DEVICE","user_code":"SYNTHETIC-CODE","verification_url":"https://trakt.tv/activate#","expires_in":600,"interval":5}`,
	} {
		t.Run("invalid-response", func(t *testing.T) {
			client, _ := authFixture(t, func(req *http.Request) (*http.Response, error) { return authFixtureResponse(200, body), nil })
			if _, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET"); err == nil || strings.Contains(err.Error(), "SYNTHETIC-SECRET") {
				t.Fatal("unsafe instructions accepted or reflected")
			}
			dir, err := authDirectory()
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("invalid response left private state")
			}
		})
	}
}

func TestAuthPollRateLimitAndOriginalClientBinding(t *testing.T) {
	polls := 0
	client, clock := authFixture(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/oauth/device/code" {
			return authFixtureResponse(200, authDeviceFixture), nil
		}
		polls++
		var body api.AuthDeviceTokenReq
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ClientID != "SYNTHETIC-ID" || body.ClientSecret != "SYNTHETIC-SECRET" || body.Code != "SYNTHETIC-PRIVATE-DEVICE" {
			t.Fatal("poll lost original credential binding")
		}
		response := authFixtureResponse(429, `{}`)
		response.Header.Set("Retry-After", "20")
		return response, nil
	})
	start, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	client.Credentials = api.Credentials{ClientID: "DIFFERENT-SYNTHETIC-ID", ClientSecret: "DIFFERENT-SYNTHETIC-SECRET"}
	*clock = clock.Add(5 * time.Second)
	output, err := pollAuthSession(client, start.Session)
	if err != nil || output.Interval != 20 || polls != 1 {
		t.Fatalf("rate limit not preserved: %+v err=%v", output, err)
	}
	*clock = clock.Add(10 * time.Second)
	if _, err := pollAuthSession(client, start.Session); err != nil || polls != 1 {
		t.Fatal("early retry contacted provider")
	}
}

func TestAuthRejectsBroadStatePermissionsAndConcurrentPoll(t *testing.T) {
	client, _ := authFixture(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/oauth/device/code" {
			t.Fatal("invalid or busy state contacted provider")
		}
		return authFixtureResponse(200, authDeviceFixture), nil
	})
	start, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := authDirectory()
	path := filepath.Join(dir, start.Session+".json")
	unlock, err := authLock(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pollAuthSession(client, start.Session); err == nil {
		t.Fatal("concurrent poll accepted")
	}
	unlock()
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := pollAuthSession(client, start.Session); err == nil {
		t.Fatal("world-readable private state accepted")
	}
}

func TestMachineAuthWindowsUnavailable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific fail-closed guard")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := authDirectory(); err == nil || !strings.Contains(err.Error(), "Windows") {
		t.Fatal("Windows machine state must fail closed until ACL privacy is implemented")
	}
}

func TestAuthSlowPollSchedulesFromResponse(t *testing.T) {
	for _, fixture := range []struct {
		name             string
		status, interval int
	}{{"pending", 400, 5}, {"rate-limit", 429, 20}} {
		t.Run(fixture.name, func(t *testing.T) {
			var clock *time.Time
			var client *api.APIClient
			polls := 0
			client, clock = authFixture(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/oauth/device/code" {
					return authFixtureResponse(200, authDeviceFixture), nil
				}
				polls++
				*clock = clock.Add(30 * time.Second)
				response := authFixtureResponse(fixture.status, `{}`)
				if fixture.status == 429 {
					response.Header.Set("Retry-After", "20")
				}
				return response, nil
			})
			start, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
			if err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(5 * time.Second)
			output, err := pollAuthSession(client, start.Session)
			if err != nil || output.Interval != fixture.interval || polls != 1 {
				t.Fatalf("response-time interval=%d, want %d, polls=%d err=%v", output.Interval, fixture.interval, polls, err)
			}
			dir, _ := authDirectory()
			state, err := readAuthState(filepath.Join(dir, start.Session+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !state.NextPollAt.Equal(clock.Add(time.Duration(fixture.interval) * time.Second)) {
				t.Fatal("next poll was scheduled from request start")
			}
			*clock = clock.Add(time.Duration(fixture.interval-1) * time.Second)
			if _, err := pollAuthSession(client, start.Session); err != nil || polls != 1 {
				t.Fatal("Retry-After was consumed by request latency")
			}
		})
	}
}

func TestAuthUnsuccessfulPollArrivingAfterExpiryFailsClosed(t *testing.T) {
	for _, status := range []int{400, 429} {
		t.Run(fmt.Sprintf("HTTP%d", status), func(t *testing.T) {
			var clock *time.Time
			var client *api.APIClient
			polls := 0
			client, clock = authFixture(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/oauth/device/code" {
					return authFixtureResponse(200, authDeviceFixture), nil
				}
				polls++
				*clock = clock.Add(30 * time.Second)
				response := authFixtureResponse(status, `{}`)
				if status == 429 {
					response.Header.Set("Retry-After", "20")
				}
				return response, nil
			})
			start, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
			if err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(590 * time.Second)
			if _, err := pollAuthSession(client, start.Session); err == nil || !strings.Contains(err.Error(), "expired") || polls != 1 {
				t.Fatal("expired delayed response was returned as pending")
			}
			dir, _ := authDirectory()
			if _, err := os.Stat(filepath.Join(dir, start.Session+".json")); !os.IsNotExist(err) {
				t.Fatal("expired state was not removed")
			}
		})
	}
}

func TestAuthGrantedRetentionStartsAtReceipt(t *testing.T) {
	for _, startAfter := range []time.Duration{5 * time.Second, 590 * time.Second} {
		t.Run(startAfter.String(), func(t *testing.T) {
			var clock *time.Time
			var client *api.APIClient
			client, clock = authFixture(t, func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/oauth/device/code" {
					return authFixtureResponse(200, authDeviceFixture), nil
				}
				*clock = clock.Add(30 * time.Second)
				return authFixtureResponse(200, authTokenFixture), nil
			})
			start, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(os.Getenv("HOME"), ".trakt.yaml"), 0700); err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(startAfter)
			if _, err := pollAuthSession(client, start.Session); err == nil {
				t.Fatal("fixture must retain a grant after failed persistence")
			}
			dir, _ := authDirectory()
			state, err := readAuthState(filepath.Join(dir, start.Session+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if state.Granted == nil || !state.ExpiresAt.Equal(clock.Add(15*time.Minute)) {
				t.Fatal("grant retention used the pre-request timestamp or discarded an issued token")
			}
		})
	}
}

func TestAuthUnknownPollDoesNotCreateLock(t *testing.T) {
	client, _ := authFixture(t, func(req *http.Request) (*http.Response, error) {
		t.Fatal("unknown session contacted provider")
		return nil, nil
	})
	if _, err := pollAuthSession(client, strings.Repeat("f", 32)); err == nil {
		t.Fatal("unknown session accepted")
	}
	dir, _ := authDirectory()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("unknown handle created a lock or state file")
	}
}

func TestAuthStartRecoversThirtyTwoStaleOrphanLocks(t *testing.T) {
	calls := 0
	client, clock := authFixture(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/oauth/device/code" {
			t.Fatal("unexpected endpoint")
		}
		return authFixtureResponse(200, authDeviceFixture), nil
	})
	dir, err := authDirectory()
	if err != nil {
		t.Fatal(err)
	}
	past := clock.Add(-10 * time.Minute)
	for i := 0; i < 32; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%032x.json.lock", i))
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET"); err != nil || calls != 1 {
		t.Fatalf("stale orphan lock recovery failed: calls=%d err=%v", calls, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".lock") {
			t.Fatal("stale orphan lock remains")
		}
	}
}

func TestAuthStaleLockSweepPreservesFreshAndUnsafeFiles(t *testing.T) {
	client, clock := authFixture(t, func(req *http.Request) (*http.Response, error) {
		return authFixtureResponse(200, authDeviceFixture), nil
	})
	dir, err := authDirectory()
	if err != nil {
		t.Fatal(err)
	}
	past := clock.Add(-10 * time.Minute)
	stale := filepath.Join(dir, strings.Repeat("1", 32)+".json.lock")
	fresh := filepath.Join(dir, strings.Repeat("2", 32)+".json.lock")
	broad := filepath.Join(dir, strings.Repeat("3", 32)+".json.lock")
	nonempty := filepath.Join(dir, strings.Repeat("4", 32)+".json.lock")
	arbitrary := filepath.Join(dir, "synthetic-unrelated.lock")
	for _, path := range []string{stale, fresh, broad, nonempty, arbitrary} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(broad, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nonempty, []byte("synthetic not-an-empty-lock"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{stale, broad, nonempty, arbitrary} {
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(t.TempDir(), "synthetic-target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, strings.Repeat("5", 32)+".json.lock")
	linked := os.Symlink(target, link) == nil
	if _, err := startAuthSession(client, "SYNTHETIC-ID", "SYNTHETIC-SECRET"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatal("safe stale lock not reclaimed")
	}
	for _, path := range []string{fresh, broad, nonempty, arbitrary} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("unsafe/fresh file removed: %s", filepath.Base(path))
		}
	}
	if linked {
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("symlink lock was removed")
		}
	}
	b, err := os.ReadFile(target)
	if err != nil || string(b) != "unchanged" {
		t.Fatal("symlink target changed")
	}
}

func TestAuthVerificationURLLengthBound(t *testing.T) {
	// Pure response validation also works without platform-specific file storage.
	response := &api.AuthDeviceCodeResp{DeviceCode: "SYNTHETIC-PRIVATE", UserCode: "SYNTHETIC-CODE", VerificationURL: "https://trakt.tv/" + strings.Repeat("a", 2048-len("https://trakt.tv/")), ExpiresIn: 600, Interval: 5}
	if !validDeviceResponse(response) {
		t.Fatal("2048-byte URL should satisfy the shared length bound")
	}
	response.VerificationURL += "a"
	if validDeviceResponse(response) {
		t.Fatal("oversized verification URL accepted")
	}
}

func TestAuthVerificationURLHosts(t *testing.T) {
	// Trakt's device-code endpoint now returns https://auth.trakt.tv/activate.
	for _, ok := range []string{"https://trakt.tv/activate", "https://www.trakt.tv/activate", "https://auth.trakt.tv/activate"} {
		response := &api.AuthDeviceCodeResp{DeviceCode: "SYNTHETIC-PRIVATE", UserCode: "SYNTHETIC-CODE", VerificationURL: ok, ExpiresIn: 600, Interval: 6}
		if !validDeviceResponse(response) {
			t.Errorf("%s rejected", ok)
		}
	}
	for _, bad := range []string{"https://auth.trakt.tv.example.com/activate", "https://evil-auth.trakt.tv.example/activate", "https://authtrakt.tv/activate", "https://auth.trakt.tv:8443/activate", "http://auth.trakt.tv/activate"} {
		response := &api.AuthDeviceCodeResp{DeviceCode: "SYNTHETIC-PRIVATE", UserCode: "SYNTHETIC-CODE", VerificationURL: bad, ExpiresIn: 600, Interval: 6}
		if validDeviceResponse(response) {
			t.Errorf("%s accepted", bad)
		}
	}
}
