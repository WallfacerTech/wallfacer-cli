package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/semver"
)

const (
	updateCheckURL = "https://api.github.com/repos/WallfacerTech/wallfacer-cli/releases/latest"
	cacheTTL       = 15 * time.Minute
	httpTimeout    = 2 * time.Second
)

type versionCache struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

type updateResult struct {
	LatestVersion  string
	CurrentVersion string
}

func versionCachePath() string {
	return filepath.Join(configDir(), "version_check.json")
}

func startUpdateCheck(currentVersion string) <-chan *updateResult {
	ch := make(chan *updateResult, 1)
	if currentVersion == "dev" {
		ch <- nil
		close(ch)
		return ch
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- nil
			}
		}()

		current := "v" + currentVersion
		if !semver.IsValid(current) {
			ch <- nil
			return
		}

		latest := ""
		if cached, err := readVersionCache(); err == nil && time.Since(cached.CheckedAt) < cacheTTL {
			latest = cached.LatestVersion
		} else {
			fetched, err := fetchLatestVersion()
			if err != nil {
				ch <- nil
				return
			}
			latest = fetched
			writeVersionCache(&versionCache{LatestVersion: latest, CheckedAt: time.Now()})
		}

		if !semver.IsValid(latest) {
			ch <- nil
			return
		}

		if semver.Compare(current, latest) < 0 {
			ch <- &updateResult{LatestVersion: latest, CurrentVersion: currentVersion}
		} else {
			ch <- nil
		}
	}()

	return ch
}

func readVersionCache() (*versionCache, error) {
	data, err := os.ReadFile(versionCachePath())
	if err != nil {
		return nil, err
	}
	var cache versionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func writeVersionCache(v *versionCache) {
	os.MkdirAll(configDir(), 0700)
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	os.WriteFile(versionCachePath(), data, 0600)
}

func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(updateCheckURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

func printUpdateMessage(result *updateResult) {
	fmt.Fprintf(os.Stderr, "\nUpdate available: %s → %s\nUpdate with:\n  curl -sSL https://raw.githubusercontent.com/WallfacerTech/wallfacer-cli/main/install.sh | sh\n", result.CurrentVersion, result.LatestVersion)
}
