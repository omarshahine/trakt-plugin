package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type persistenceTransport func(*http.Request) (*http.Response, error)

func (fn persistenceTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }
func persistenceResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestRefreshPropagatesCredentialWriteFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.Mkdir(filepath.Join(home, ".trakt.yaml"), 0700); err != nil {
		t.Fatal(err)
	}
	client := NewAPIClientWithCredentials(Credentials{ClientID: "SYNTHETIC-ID", ClientSecret: "SYNTHETIC-SECRET", RefreshToken: "SYNTHETIC-OLD"})
	calls := 0
	client.Client = &http.Client{Transport: persistenceTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path != "/oauth/token" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		return persistenceResponse(200, `{"access_token":"SYNTHETIC-ACCESS","refresh_token":"SYNTHETIC-ROTATED"}`), nil
	})}
	if err := client.RefreshAccessToken(); err == nil {
		t.Fatal("refresh must fail if rotated credentials cannot be saved")
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	info, err := os.Stat(filepath.Join(home, ".trakt.yaml"))
	if err != nil || !info.IsDir() {
		t.Fatal("fixture destination changed")
	}
}

func TestAutoRefreshDoesNotHidePersistenceFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.Mkdir(filepath.Join(home, ".trakt.yaml"), 0700); err != nil {
		t.Fatal(err)
	}
	client := NewAPIClientWithCredentials(Credentials{ClientID: "SYNTHETIC-ID", AccessToken: "SYNTHETIC-OLD", RefreshToken: "SYNTHETIC-REFRESH"})
	calls := 0
	client.Client = &http.Client{Transport: persistenceTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path == "/oauth/token" {
			return persistenceResponse(200, `{"access_token":"SYNTHETIC-NEW","refresh_token":"SYNTHETIC-ROTATED"}`), nil
		}
		if calls != 1 {
			t.Fatal("must not retry the account request after failed persistence")
		}
		return persistenceResponse(401, `{}`), nil
	})}
	resp, err := client.doRequest(requestParams{method: http.MethodGet, path: "/synthetic-account-read", auth: true})
	if err == nil || resp != nil || calls != 2 {
		t.Fatalf("expected propagated persistence failure, calls=%d err=%v", calls, err)
	}
}

func TestSaveCredentialsReplacesPrivatelyAndRejectsLinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".trakt.yaml")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(Credentials{AccessToken: "SYNTHETIC-ACCESS"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode=%v", info.Mode())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "synthetic-other")
	if err := os.WriteFile(other, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := SaveCredentials(Credentials{AccessToken: "SYNTHETIC-NEW"}); err == nil {
		t.Fatal("must reject symlink destination")
	}
	contents, err := os.ReadFile(other)
	if err != nil || string(contents) != "unchanged" {
		t.Fatal("symlink target changed")
	}
}

func TestSlugCacheWriteIsExplicitlyBestEffort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.Mkdir(filepath.Join(home, ".trakt.yaml"), 0700); err != nil {
		t.Fatal(err)
	}
	client := NewAPIClientWithCredentials(Credentials{AccessToken: "SYNTHETIC"})
	client.Client = &http.Client{Transport: persistenceTransport(func(req *http.Request) (*http.Response, error) {
		return persistenceResponse(200, `{"user":{"ids":{"slug":"synthetic-user"}}}`), nil
	})}
	slug, err := client.GetUserSlug()
	if err != nil || slug != "synthetic-user" {
		t.Fatalf("slug=%q err=%v", slug, err)
	}
}

func TestOAuthErrorsDoNotEchoProviderBodies(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	client := NewAPIClientWithCredentials(Credentials{RefreshToken: "SYNTHETIC-OLD"})
	client.Client = &http.Client{Transport: persistenceTransport(func(req *http.Request) (*http.Response, error) {
		return persistenceResponse(403, `{"error":"SYNTHETIC-SECRET"}`), nil
	})}
	err := client.RefreshAccessToken()
	if err == nil || strings.Contains(err.Error(), "SYNTHETIC-SECRET") {
		t.Fatalf("unsafe error: %v", err)
	}
	_, err = client.AuthDeviceToken(&AuthDeviceTokenReq{})
	if err == nil || strings.Contains(err.Error(), "SYNTHETIC-SECRET") {
		t.Fatalf("unsafe error: %v", err)
	}
	_, err = client.AuthDeviceCode(&AuthDeviceCodeReq{})
	if err == nil || strings.Contains(err.Error(), "SYNTHETIC-SECRET") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestOAuthDecodeErrorsDoNotExposeResponseValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const marker = "123456789012345678901234567890"
	client := NewAPIClientWithCredentials(Credentials{RefreshToken: "SYNTHETIC-OLD"})
	client.Client = &http.Client{Transport: persistenceTransport(func(req *http.Request) (*http.Response, error) {
		field := "access_token"
		if req.URL.Path == "/oauth/device/code" {
			field = "device_code"
		}
		return persistenceResponse(200, `{"`+field+`":`+marker+`}`), nil
	})}
	err := client.RefreshAccessToken()
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("unsafe decode error: %v", err)
	}
	_, err = client.AuthDeviceCode(&AuthDeviceCodeReq{})
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("unsafe decode error: %v", err)
	}
	_, err = client.AuthDeviceToken(&AuthDeviceTokenReq{})
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("unsafe decode error: %v", err)
	}
}
