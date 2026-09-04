package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func directAPITestServer(t *testing.T, directAPI bool) (*httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:admin\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	staticDir := filepath.Join(dir, "www")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatalf("make static dir: %v", err)
	}
	cfg := serverConfig{
		staticDir: staticDir,
		authFile:  authFile,
		ttlFile:   filepath.Join(dir, "ttlvalue"),
		noTLS:     true,
		mockMode:  true,
		directAPI: directAPI,
	}
	srv := &simpleAdminServer{cfg: cfg}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return ts, &http.Client{Jar: jar}
}

func directAPILogin(t *testing.T, ts *httptest.Server, client *http.Client) {
	t.Helper()
	form := url.Values{"username": {"admin"}, "password": {"admin"}}
	resp, err := client.PostForm(ts.URL+"/api/login", form)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
}

func TestDirectAPIDisabledKeepsGatewayOnlyBehavior(t *testing.T) {
	ts, client := directAPITestServer(t, false)
	directAPILogin(t, ts, client)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, err := http.NewRequest(method, ts.URL+"/api/dashboard_data", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s dashboard_data: %v", method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s dashboard_data status = %d, want 404", method, resp.StatusCode)
		}
	}
}

func TestDirectAPIEnabledRequiresLogin(t *testing.T) {
	ts, client := directAPITestServer(t, true)

	resp, err := client.Get(ts.URL + "/api/dashboard_data")
	if err != nil {
		t.Fatalf("GET dashboard_data: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dashboard_data status = %d, want 401", resp.StatusCode)
	}
}

func TestDirectAPIEnabledServesDashboardData(t *testing.T) {
	ts, client := directAPITestServer(t, true)
	directAPILogin(t, ts, client)

	resp, err := client.PostForm(ts.URL+"/api/dashboard_data", url.Values{})
	if err != nil {
		t.Fatalf("POST dashboard_data: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard_data status = %d, want 200, body = %s", resp.StatusCode, body)
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("dashboard_data body not json: %v, body = %s", err, body)
	}
	if got := data["network_provider"]; got == nil || strings.TrimSpace(got.(string)) == "" {
		t.Fatalf("dashboard_data network_provider = %#v, want mock value", got)
	}
}
