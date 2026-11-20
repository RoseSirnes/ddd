package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateServerURL(t *testing.T) {
	good := []string{"https://chat.example.com", "https://my-app.onrender.com/"}
	for _, v := range good {
		if _, err := validateServerURL(v); err != nil {
			t.Errorf("rejected valid url %q: %v", v, err)
		}
	}
	bad := []string{"http://chat.example.com", "https://127.0.0.1", "https://localhost", "https://example.com:8989", "https://example.com/phishing", "https://name:password@example.com", "https://example.com?redirect=evil", "https://localhost.local"}
	for _, v := range bad {
		if _, err := validateServerURL(v); err == nil {
			t.Errorf("accepted unsafe url %q", v)
		}
	}
}
func TestServerConfigPersist(t *testing.T) {
	dir := t.TempDir()
	if err := writeServerConfig(dir, serverConfig{URL: "https://chat.example.com"}); err != nil {
		t.Fatal(err)
	}
	got, err := readServerConfig(dir)
	if err != nil || got.URL != "https://chat.example.com" {
		t.Fatalf("config %v error %v", got, err)
	}
	if err := writeServerConfig(dir, serverConfig{}); err != nil {
		t.Fatal(err)
	}
	got, err = readServerConfig(dir)
	if err != nil || got.URL != "" {
		t.Fatalf("clear %v error %v", got, err)
	}
}

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCentralProxyRetainsBackendResponseAndLocalAdminIsolation(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeServerConfig(dir, serverConfig{URL: "https://chat.example.com"}); err != nil {
		t.Fatal(err)
	}
	orig := centralTransportFactory
	defer func() { centralTransportFactory = orig }()
	calls := 0
	centralTransportFactory = func() http.RoundTripper {
		return fakeRoundTripper(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.Host != "chat.example.com" || req.URL.Scheme != "https" || req.URL.Path != "/api/me" {
				t.Errorf("wrong upstream %s", req.URL.String())
			}
			if !strings.Contains(req.Header.Get("Cookie"), "session-test") {
				t.Errorf("missing client cookie")
			}
			hdr := http.Header{}
			hdr.Add("Set-Cookie", "ddd_session=session-test-2; Path=/; Secure; HttpOnly; SameSite=Lax")
			hdr.Set("Content-Type", "application/json")
			return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader(`{"user":{"nickname":"Ana"}}`))}, nil
		})
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:45000/api/me", nil)
	req.Header.Set("Cookie", "ddd_session=session-test")
	rec := httptest.NewRecorder()
	a.routes(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ana") {
		t.Fatalf("bad remote response %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Header().Get("Set-Cookie"), "Secure") {
		t.Errorf("loopback cookie must not require HTTPS")
	}
	rec2 := httptest.NewRecorder()
	a.routes(rec2, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:45000/api/update/status", nil))
	if rec2.Code != 401 || calls != 1 {
		t.Fatalf("local admin endpoint unexpectedly proxied %d calls=%d", rec2.Code, calls)
	}
}
