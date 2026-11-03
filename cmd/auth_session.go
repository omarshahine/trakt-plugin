package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
)

const maxAuthSessions = 8
const maxAuthStateBytes = 16 * 1024
const maxAuthCleanupEntries = 256
const authLockLifetime = 5 * time.Minute

var authSessionPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var authUserCodePattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)
var authNow = time.Now

// Only these public fields may reach an agent. Device codes, client secrets
// and bearer tokens stay in owner-private files and never enter this type.
type authOutput struct {
	Success         bool   `json:"success"`
	Status          string `json:"status"`
	Session         string `json:"session,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	UserCode        string `json:"user_code,omitempty"`
	ExpiresIn       int    `json:"expires_in,omitempty"`
	Interval        int    `json:"interval,omitempty"`
}

type authState struct {
	Version      int       `json:"version"`
	DeviceCode   string    `json:"device_code"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	ExpiresAt    time.Time `json:"expires_at"`
	NextPollAt   time.Time `json:"next_poll_at"`
	Interval     int       `json:"interval"`
	// If the credential destination fails, retain the newly issued token pair
	// privately so another poll can retry persistence without reusing the code.
	Granted *api.Credentials `json:"granted,omitempty"`
}

func validDeviceResponse(resp *api.AuthDeviceCodeResp) bool {
	if resp == nil || resp.DeviceCode == "" || len(resp.DeviceCode) > 4096 || len(resp.VerificationURL) > 2048 || !authUserCodePattern.MatchString(resp.UserCode) || resp.ExpiresIn <= 0 || resp.ExpiresIn > 3600 || resp.Interval <= 0 || resp.Interval > 60 {
		return false
	}
	if strings.ContainsAny(resp.VerificationURL, "?#") {
		return false
	}
	u, err := url.Parse(resp.VerificationURL)
	return err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && (u.Hostname() == "trakt.tv" || u.Hostname() == "www.trakt.tv") && (u.Port() == "" || u.Port() == "443")
}

func publicDeviceResponse(resp *api.AuthDeviceCodeResp, clientSecret string) bool {
	if !validDeviceResponse(resp) {
		return false
	}
	// Compare the decoded URL path too, so percent-encoding cannot conceal
	// a reflected credential. Query strings and fragments are forbidden.
	parsed, _ := url.Parse(resp.VerificationURL)
	// Refuse provider mistakes that reflect private input in public fields.
	for _, value := range []string{resp.DeviceCode, clientSecret} {
		if value != "" && (strings.Contains(resp.UserCode, value) || strings.Contains(resp.VerificationURL, value) || strings.Contains(parsed.Path, value)) {
			return false
		}
	}
	return true
}

func authDirectory() (string, error) {
	// Go's mode bits do not establish Windows ACL privacy. Fail closed for
	// the new session cache until an owner-only ACL implementation is tested.
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("machine authentication is unavailable on Windows; run plain trakt-cli auth locally")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("private authentication directory unavailable")
	}
	if info, err := os.Lstat(home); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (runtime.GOOS != "windows" && info.Mode().Perm()&0022 != 0) {
		return "", fmt.Errorf("unsafe authentication home directory")
	}
	current := home
	for _, part := range []string{".config", "trakt-cli", "auth-sessions"} {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
			return "", fmt.Errorf("private authentication directory unavailable")
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("unsafe authentication directory")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0022 != 0 {
			return "", fmt.Errorf("authentication parent directory is writable by others")
		}
		if part == "auth-sessions" && runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
			return "", fmt.Errorf("authentication directory must be private")
		}
	}
	return current, nil
}

// The private directory is owned by the existing OS principal. Lstat checks
// reject links; opened-file identity checks stop substitution before reads.
// This does not claim isolation from an already-compromised same-UID process.
func readAuthState(path string) (authState, error) {
	var state authState
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxAuthStateBytes || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return state, fmt.Errorf("invalid authentication session")
	}
	file, err := os.Open(path)
	if err != nil {
		return state, fmt.Errorf("authentication session unavailable")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return state, fmt.Errorf("invalid authentication session")
	}
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return state, fmt.Errorf("invalid authentication session")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAuthStateBytes+1))
	if err != nil || len(data) > maxAuthStateBytes {
		return state, fmt.Errorf("invalid authentication session")
	}
	if err := json.Unmarshal(data, &state); err != nil || state.Version != 1 || state.ClientID == "" || state.ClientSecret == "" || state.DeviceCode == "" || state.Interval < 1 || state.Interval > 60 || state.ExpiresAt.IsZero() {
		return authState{}, fmt.Errorf("invalid authentication session")
	}
	return state, nil
}

func writeAuthState(path string, state authState, create bool) error {
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxAuthStateBytes {
		return fmt.Errorf("invalid authentication session")
	}
	if create {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("cannot create private authentication session")
		}
		ok := false
		defer func() {
			file.Close()
			if !ok {
				os.Remove(path)
			}
		}()
		if _, err = file.Write(data); err != nil {
			return fmt.Errorf("cannot save authentication session")
		}
		if err = file.Sync(); err != nil {
			return fmt.Errorf("cannot save authentication session")
		}
		if err = file.Close(); err != nil {
			return fmt.Errorf("cannot save authentication session")
		}
		if err := syncAuthDirectory(filepath.Dir(path)); err != nil {
			return err
		}
		ok = true
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("invalid authentication session")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return fmt.Errorf("cannot save authentication session")
	}
	temp := file.Name()
	defer os.Remove(temp)
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return fmt.Errorf("cannot save authentication session")
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("cannot save authentication session")
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("cannot save authentication session")
	}
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return fmt.Errorf("invalid authentication session")
	}
	if err = os.Rename(temp, path); err != nil {
		return fmt.Errorf("cannot save authentication session")
	}
	return syncAuthDirectory(filepath.Dir(path))
}

func syncAuthDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot sync authentication session directory")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("cannot sync authentication session directory")
	}
	return nil
}

func authLock(path string) (func(), error) {
	// Locks live longer than the normal 120-second API deadline. Stale recovery
	// only removes a regular file in this existing owner-private namespace.
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			file.Close()
			return func() { os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("authentication session lock unavailable")
		}
		info, err := os.Lstat(path)
		if err != nil || !safeStaleAuthLock(info) {
			return nil, fmt.Errorf("authentication session busy; try again later")
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("authentication session lock unavailable")
		}
	}
	return nil, fmt.Errorf("authentication session busy; try again later")
}

func safeStaleAuthLock(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0 && info.Size() == 0 && authNow().Sub(info.ModTime()) >= authLockLifetime
}

// Sweep only generated per-session lock names, never .create.lock, session
// state or arbitrary files. The scan is bounded; repeated starts can make
// progress through an old oversized namespace without unbounded work. The
// namespace is owner-private; same-UID tampering is not a separate boundary.
func cleanupStaleAuthLocks(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("authentication directory unavailable")
	}
	entries, readErr := directory.ReadDir(maxAuthCleanupEntries)
	directory.Close()
	if readErr != nil && readErr != io.EOF {
		return fmt.Errorf("authentication directory unavailable")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json.lock") {
			continue
		}
		handle := strings.TrimSuffix(entry.Name(), ".json.lock")
		if !authSessionPattern.MatchString(handle) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !safeStaleAuthLock(info) {
			continue
		}
		current, err := os.Lstat(path)
		if err == nil && safeStaleAuthLock(current) && os.SameFile(info, current) {
			// Failure keeps the file subject to the normal post-cleanup cap.
			_ = os.Remove(path)
		}
	}
	return nil
}

func pendingAuth(handle string, state authState) authOutput {
	now := authNow()
	wait := int(math.Ceil(state.NextPollAt.Sub(now).Seconds()))
	if wait < 1 {
		wait = 1
	}
	expires := int(math.Ceil(state.ExpiresAt.Sub(now).Seconds()))
	if expires < 1 {
		expires = 1
	}
	return authOutput{Success: true, Status: "pending", Session: handle, ExpiresIn: expires, Interval: wait}
}

func startAuthSession(client *api.APIClient, clientID, clientSecret string) (authOutput, error) {
	if clientID == "" || clientSecret == "" || len(clientID) > 1024 || len(clientSecret) > 4096 {
		return authOutput{}, fmt.Errorf("invalid authentication client configuration")
	}
	dir, err := authDirectory()
	if err != nil {
		return authOutput{}, err
	}
	unlock, err := authLock(filepath.Join(dir, ".create.lock"))
	if err != nil {
		return authOutput{}, err
	}
	defer unlock()
	if err := cleanupStaleAuthLocks(dir); err != nil {
		return authOutput{}, err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return authOutput{}, fmt.Errorf("authentication directory unavailable")
	}
	entries, readErr := directory.ReadDir(maxAuthSessions*4 + 1)
	directory.Close()
	if (readErr != nil && readErr != io.EOF) || len(entries) > maxAuthSessions*4 {
		return authOutput{}, fmt.Errorf("too many authentication session files")
	}
	count := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		handle := strings.TrimSuffix(entry.Name(), ".json")
		if !authSessionPattern.MatchString(handle) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		state, stateErr := readAuthState(path)
		if stateErr == nil && !authNow().Before(state.ExpiresAt) {
			// Serialize cleanup with polling, including recovery of a stale
			// crashed poll lock. Re-read after locking before deleting state.
			if release, lockErr := authLock(path + ".lock"); lockErr == nil {
				current, readErr := readAuthState(path)
				removed := readErr == nil && !authNow().Before(current.ExpiresAt) && os.Remove(path) == nil
				release()
				if removed {
					continue
				}
			}
		}
		count++
	}
	if count >= maxAuthSessions {
		return authOutput{}, fmt.Errorf("too many pending authentication sessions; wait for expiry")
	}
	response, err := client.AuthDeviceCode(&api.AuthDeviceCodeReq{ClientID: clientID})
	if err != nil || !publicDeviceResponse(response, clientSecret) {
		return authOutput{}, fmt.Errorf("failed to start Trakt authentication")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return authOutput{}, fmt.Errorf("cannot create authentication session")
	}
	handle := hex.EncodeToString(random[:])
	now := authNow()
	state := authState{Version: 1, DeviceCode: response.DeviceCode, ClientID: clientID, ClientSecret: clientSecret, ExpiresAt: now.Add(time.Duration(response.ExpiresIn) * time.Second), NextPollAt: now.Add(time.Duration(response.Interval) * time.Second), Interval: response.Interval}
	if err := writeAuthState(filepath.Join(dir, handle+".json"), state, true); err != nil {
		return authOutput{}, err
	}
	output := pendingAuth(handle, state)
	output.VerificationURL = response.VerificationURL
	output.UserCode = response.UserCode
	return output, nil
}

func pollAuthSession(client *api.APIClient, handle string) (authOutput, error) {
	if !authSessionPattern.MatchString(handle) {
		return authOutput{}, fmt.Errorf("invalid authentication session handle")
	}
	dir, err := authDirectory()
	if err != nil {
		return authOutput{}, err
	}
	path := filepath.Join(dir, handle+".json")
	// Unknown handles must not create persistent lock files if the process
	// is interrupted. Re-read under the lock before using accepted state.
	if _, err := readAuthState(path); err != nil {
		return authOutput{}, err
	}
	unlock, err := authLock(path + ".lock")
	if err != nil {
		return authOutput{}, err
	}
	defer unlock()
	state, err := readAuthState(path)
	if err != nil {
		return authOutput{}, err
	}
	now := authNow()
	if !now.Before(state.ExpiresAt) {
		os.Remove(path)
		return authOutput{}, fmt.Errorf("authentication session expired; start again")
	}
	if state.Granted == nil {
		if now.Before(state.NextPollAt) {
			return pendingAuth(handle, state), nil
		}
		// Save the rate-limit decision before making the one network attempt.
		state.NextPollAt = now.Add(time.Duration(state.Interval) * time.Second)
		if err := writeAuthState(path, state, false); err != nil {
			return authOutput{}, err
		}
		token, err := client.AuthDeviceToken(&api.AuthDeviceTokenReq{Code: state.DeviceCode, ClientID: state.ClientID, ClientSecret: state.ClientSecret})
		receivedAt := authNow()
		if err != nil {
			if !receivedAt.Before(state.ExpiresAt) {
				_ = os.Remove(path)
				return authOutput{}, fmt.Errorf("authentication session expired; start again")
			}
			var limited *api.RateLimitError
			if errors.As(err, &limited) {
				// A slow-down response increases the baseline even when the
				// provider omits Retry-After. Keep future polling bounded.
				if state.Interval < 60 {
					state.Interval += 5
					if state.Interval > 60 {
						state.Interval = 60
					}
				}
				wait := limited.RetryAfterSeconds
				if wait < state.Interval {
					wait = state.Interval
				}
				remaining := int(math.Ceil(state.ExpiresAt.Sub(receivedAt).Seconds()))
				if wait > remaining {
					wait = remaining
				}
				state.NextPollAt = receivedAt.Add(time.Duration(wait) * time.Second)
				if state.NextPollAt.After(state.ExpiresAt) {
					state.NextPollAt = state.ExpiresAt
				}
				if err := writeAuthState(path, state, false); err != nil {
					return authOutput{}, err
				}
				return pendingAuth(handle, state), nil
			}
			return authOutput{}, fmt.Errorf("Trakt authentication failed; start again")
		}
		if token.AccessToken == "" {
			if !receivedAt.Before(state.ExpiresAt) {
				_ = os.Remove(path)
				return authOutput{}, fmt.Errorf("authentication session expired; start again")
			}
			state.NextPollAt = receivedAt.Add(time.Duration(state.Interval) * time.Second)
			if state.NextPollAt.After(state.ExpiresAt) {
				state.NextPollAt = state.ExpiresAt
			}
			if err := writeAuthState(path, state, false); err != nil {
				return authOutput{}, err
			}
			return pendingAuth(handle, state), nil
		}
		state.Granted = &api.Credentials{ClientID: state.ClientID, ClientSecret: state.ClientSecret, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken}
		// Retain a failed persistence retry privately for at most fifteen minutes.
		// Honor a grant accepted by the provider before expiry even if its
		// response arrived afterward; never discard an issued token pair.
		state.ExpiresAt = receivedAt.Add(15 * time.Minute)
		if err := writeAuthState(path, state, false); err != nil {
			return authOutput{}, err
		}
	}
	if err := persistAuthCredentials(*state.Granted); err != nil {
		return authOutput{}, err
	}
	if err := os.Remove(path); err != nil {
		return authOutput{}, fmt.Errorf("authenticated, but private session cleanup failed")
	}
	return authOutput{Success: true, Status: "authenticated"}, nil
}
