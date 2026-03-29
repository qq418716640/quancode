package version

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	checkInterval = 2 * time.Hour
	httpTimeout   = 10 * time.Second
	releasesURL   = "https://github.com/qq418716640/quancode/releases/latest"
	downloadURL   = "https://github.com/qq418716640/quancode/releases/download/%s/quancode_%s_%s_%s.tar.gz"
)

type updateCache struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
}

var (
	updateNotice string
	noticeMu     sync.Mutex
)

// UpdateNotice returns a pending update message, if any.
func UpdateNotice() string {
	noticeMu.Lock()
	defer noticeMu.Unlock()
	return updateNotice
}

// BackgroundUpdate checks for and applies updates in the background.
// Safe to call from a goroutine. Never blocks the caller.
func BackgroundUpdate() {
	if Version == "dev" {
		debugf("skipping update check: dev version")
		return
	}
	if os.Getenv("QUANCODE_SKIP_UPDATE_CHECK") != "" {
		debugf("skipping update check: QUANCODE_SKIP_UPDATE_CHECK set")
		return
	}

	cache, cpath := loadCache()

	// Check if we recently checked
	if time.Since(cache.LastCheck) < checkInterval {
		if cache.LatestVersion != "" && cache.LatestVersion != Version {
			setNotice(cache.LatestVersion)
		}
		return
	}

	latest := fetchLatestVersion()
	if latest == "" {
		return
	}

	// Update cache
	cache.LastCheck = time.Now()
	cache.LatestVersion = latest
	saveCache(cpath, cache)

	if latest == Version {
		return
	}

	// Perform update
	if err := performUpdate(latest); err != nil {
		debugf("update failed: %v", err)
		return
	}
	setNotice(latest)
}

func setNotice(latest string) {
	noticeMu.Lock()
	defer noticeMu.Unlock()
	updateNotice = fmt.Sprintf("[quancode] updated to %s (current process is still %s, takes effect on next launch)", latest, Version)
}

func updateCachePath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "quancode", "update_check.json")
	}
	return ""
}

func loadCache() (updateCache, string) {
	p := updateCachePath()
	if p == "" {
		return updateCache{}, ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return updateCache{}, p
	}
	var c updateCache
	json.Unmarshal(data, &c)
	return c, p
}

func saveCache(path string, c updateCache) {
	if path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.Marshal(c)
	os.WriteFile(path, data, 0644)
}

// fetchLatestVersion queries GitHub for the latest release tag.
func fetchLatestVersion() string {
	client := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(releasesURL)
	if err != nil {
		debugf("update check failed: %v", err)
		return ""
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		debugf("update check: unexpected status %d", resp.StatusCode)
		return ""
	}

	loc := resp.Header.Get("Location")
	if idx := strings.LastIndex(loc, "/"); idx >= 0 {
		return loc[idx+1:]
	}
	return ""
}

func performUpdate(tag string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	if isBrewInstall(exe) {
		return brewUpgrade()
	}
	return downloadAndReplace(tag, exe)
}

func isBrewInstall(exePath string) bool {
	return strings.Contains(exePath, "/Cellar/") || strings.Contains(exePath, "/homebrew/")
}

func brewUpgrade() error {
	cmd := exec.Command("brew", "upgrade", "quancode")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func downloadAndReplace(tag, exePath string) error {
	ver := strings.TrimPrefix(tag, "v")
	url := fmt.Sprintf(downloadURL, tag, ver, runtime.GOOS, runtime.GOARCH)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	binary, err := extractBinaryFromTarGz(resp.Body)
	if err != nil {
		return err
	}

	// Atomic replace: write to temp file, then rename
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, "quancode-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	info, err := os.Stat(exePath)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func debugf(format string, args ...any) {
	if os.Getenv("QUANCODE_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[quancode:debug] "+format+"\n", args...)
	}
}

func extractBinaryFromTarGz(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == "quancode" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("quancode binary not found in archive")
}
