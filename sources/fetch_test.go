package sources

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withInsecureClient lets fetch talk to httptest TLS servers.
func withInsecureClient(t *testing.T) {
	t.Helper()
	orig := httpClient.Transport
	httpClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // test-only
	t.Cleanup(func() { httpClient.Transport = orig })
}

func TestFetchDropsCredentialsOnCrossSiteRedirect(t *testing.T) {
	withInsecureClient(t)
	var gotCookie, gotAuth string
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie, gotAuth = r.Header.Get("Cookie"), r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer other.Close()
	// Reach the second server as "localhost" so it is a different site
	// from the first ("127.0.0.1").
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	start := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, otherURL+"/landing", http.StatusFound)
	}))
	defer start.Close()

	resp, err := fetch(context.Background(), start.URL, map[string]string{"Cookie": "session=secret", "Authorization": "Bearer secret"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Fatalf("status %d", resp.Status)
	}
	if gotCookie != "" || gotAuth != "" {
		t.Errorf("credentials leaked across sites: cookie=%q auth=%q", gotCookie, gotAuth)
	}
}

func TestFetchKeepsCredentialsSameSite(t *testing.T) {
	withInsecureClient(t)
	var gotCookie string
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		gotCookie = r.Header.Get("Cookie")
	}))
	defer srv.Close()
	if _, err := fetch(context.Background(), srv.URL+"/start", map[string]string{"Cookie": "session=ok"}); err != nil {
		t.Fatal(err)
	}
	if gotCookie != "session=ok" {
		t.Errorf("same-site redirect should keep cookie, got %q", gotCookie)
	}
}

func TestFetchRefusesHTTPRedirect(t *testing.T) {
	withInsecureClient(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/x?token=abc", http.StatusFound)
	}))
	defer srv.Close()
	_, err := fetch(context.Background(), srv.URL+"/?tkn=SECRETVALUE", map[string]string{"Cookie": "a=b"})
	if err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("want non-https refusal, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRETVALUE") {
		t.Errorf("error leaked query secret: %v", err)
	}
}

func TestFetchErrorsOmitQuery(t *testing.T) {
	// Unroutable port: transport error must not echo the token.
	_, err := fetch(context.Background(), "https://127.0.0.1:1/SwitchProfile?tkn=SECRETVALUE", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRETVALUE") {
		t.Errorf("error leaked query secret: %v", err)
	}
}

func TestSiteOf(t *testing.T) {
	cases := map[string]string{
		"www.netflix.com":                   "netflix.com",
		"default.prd.api.hbomax.com":        "hbomax.com",
		"tv.apple.com":                      "apple.com",
		"localhost":                         "localhost",
		"evil-netflix.com.attacker.example": "attacker.example",
	}
	for in, want := range cases {
		if got := siteOf(in); got != want {
			t.Errorf("siteOf(%q) = %q, want %q", in, got, want)
		}
	}
}
