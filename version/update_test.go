package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchLatestVersionRedirectParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://github.com/qq418716640/quancode/releases/tag/v0.9.0", http.StatusFound)
	}))
	defer srv.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	idx := strings.LastIndex(loc, "/")
	tag := loc[idx+1:]
	if tag != "v0.9.0" {
		t.Fatalf("expected v0.9.0, got %q", tag)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update_check.json")

	c := updateCache{
		LastCheck:     time.Now().Truncate(time.Second),
		LatestVersion: "v1.0.0",
	}
	saveCache(path, c)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var loaded updateCache
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.LatestVersion != "v1.0.0" {
		t.Fatalf("expected v1.0.0, got %q", loaded.LatestVersion)
	}
	if loaded.LastCheck.IsZero() {
		t.Fatal("expected non-zero last_check")
	}
}

func TestIsBrewInstall(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/quancode/0.4.12/bin/quancode", true},
		{"/usr/local/homebrew/Cellar/quancode/0.4.12/bin/quancode", true},
		{"/home/user/go/bin/quancode", false},
		{"/usr/local/bin/quancode", false},
	}
	for _, tt := range tests {
		got := isBrewInstall(tt.path)
		if got != tt.want {
			t.Errorf("isBrewInstall(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestBackgroundUpdateSkipsDevVersion(t *testing.T) {
	orig := Version
	Version = "dev"
	defer func() { Version = orig }()

	noticeMu.Lock()
	updateNotice = ""
	noticeMu.Unlock()

	BackgroundUpdate()

	if notice := UpdateNotice(); notice != "" {
		t.Fatalf("expected no notice for dev version, got %q", notice)
	}
}

func TestBackgroundUpdateSkipsWithEnvVar(t *testing.T) {
	orig := Version
	Version = "v0.1.0"
	defer func() { Version = orig }()

	noticeMu.Lock()
	updateNotice = ""
	noticeMu.Unlock()

	t.Setenv("QUANCODE_SKIP_UPDATE_CHECK", "1")

	BackgroundUpdate()

	if notice := UpdateNotice(); notice != "" {
		t.Fatalf("expected no notice with skip env, got %q", notice)
	}
}

func TestSetNotice(t *testing.T) {
	orig := Version
	Version = "v0.4.12"
	defer func() { Version = orig }()

	noticeMu.Lock()
	updateNotice = ""
	noticeMu.Unlock()

	setNotice("v0.5.0")
	notice := UpdateNotice()
	if notice == "" {
		t.Fatal("expected notice")
	}
	if !strings.Contains(notice, "v0.5.0") || !strings.Contains(notice, "v0.4.12") {
		t.Fatalf("notice should mention both versions, got %q", notice)
	}
}
