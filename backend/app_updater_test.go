package backend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchLatestReleaseFallsBackToGithubLatestRedirectWhenAPIRateLimited(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "API rate limit exceeded", http.StatusForbidden)
	})
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/sdohuajia/BoostBrowser/releases/tag/v9.9.9", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	rel, err := fetchLatestReleaseWithFallback(client, srv.URL+"/api/releases/latest", srv.URL+"/releases/latest")
	if err != nil {
		t.Fatalf("fetchLatestReleaseWithFallback returned error: %v", err)
	}
	if rel.TagName != "v9.9.9" {
		t.Fatalf("expected fallback tag v9.9.9, got %q", rel.TagName)
	}

	var exeURL, shaURL string
	for _, asset := range rel.Assets {
		switch asset.Name {
		case "boost-browser.exe":
			exeURL = asset.BrowserDownloadURL
		case "boost-browser.exe.sha256":
			shaURL = asset.BrowserDownloadURL
		}
	}
	if !strings.Contains(exeURL, "/releases/download/v9.9.9/boost-browser.exe") {
		t.Fatalf("fallback exe asset URL not constructed from tag: %q", exeURL)
	}
	if !strings.Contains(shaURL, "/releases/download/v9.9.9/boost-browser.exe.sha256") {
		t.Fatalf("fallback sha asset URL not constructed from tag: %q", shaURL)
	}
}
