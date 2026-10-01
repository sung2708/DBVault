// Package update checks official DBVault release metadata. It never downloads
// or installs release assets.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	ModulePath     = "github.com/sung2708/DBVault/cmd/dbvault"
	ReleaseAPI     = "https://api.github.com/repos/sung2708/DBVault/releases/latest"
	CacheTTL       = 24 * time.Hour
	RequestTimeout = 8 * time.Second
	maxResponse    = 1 << 20
)

type State string

const (
	Development     State = "development"
	UpdateAvailable State = "update_available"
	UpToDate        State = "up_to_date"
	InstalledNewer  State = "installed_newer"
	VersionUnknown  State = "version_unknown"
	CheckFailed     State = "check_failed"
)

type Release struct {
	Version     string    `json:"version"`
	URL         string    `json:"release_url"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}

type Result struct {
	InstalledVersion string    `json:"installed_version"`
	LatestVersion    string    `json:"latest_version,omitempty"`
	State            State     `json:"state"`
	UpdateAvailable  bool      `json:"update_available"`
	ReleaseURL       string    `json:"release_url,omitempty"`
	UpdateCommand    string    `json:"update_command,omitempty"`
	MajorUpgrade     bool      `json:"major_upgrade,omitempty"`
	CheckedAt        time.Time `json:"checked_at,omitempty"`
	CacheHit         bool      `json:"cache_hit,omitempty"`
	Reason           string    `json:"reason,omitempty"`
}

type CheckError struct {
	Result Result
	Err    error
}

func (e *CheckError) Error() string { return e.Err.Error() }
func (e *CheckError) Unwrap() error { return e.Err }

type ReleaseChecker interface {
	Latest(context.Context) (Release, error)
}

type Service interface {
	Check(context.Context, string, bool) (Result, error)
}

type Checker struct {
	Client    *http.Client
	Releases  ReleaseChecker
	CachePath string
	Endpoint  string
	Now       func() time.Time
}

type apiRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

type cacheEntry struct {
	Release   Release   `json:"release"`
	CheckedAt time.Time `json:"checked_at"`
}

func (c *Checker) Latest(ctx context.Context) (Release, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: RequestTimeout}
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = ReleaseAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "DBVault/update-check")
	origin, err := url.Parse(endpoint)
	if err != nil {
		return Release{}, errors.New("invalid official release API URL")
	}
	clientCopy := *client
	previousRedirect := client.CheckRedirect
	clientCopy.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.Scheme != origin.Scheme || next.URL.Host != origin.Host {
			return http.ErrUseLastResponse
		}
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	resp, err := clientCopy.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Release{}, errors.New("request timed out")
		}
		if errors.Is(err, context.Canceled) {
			return Release{}, err
		}
		return Release{}, errors.New("network request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return Release{}, errors.New("GitHub API rate limit reached; try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if errors.Is(err, context.Canceled) {
		return Release{}, err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Release{}, errors.New("request timed out")
	}
	if err != nil {
		return Release{}, errors.New("GitHub API response was unreadable")
	}
	if len(body) > maxResponse {
		return Release{}, errors.New("GitHub API response was too large or unreadable")
	}
	var data apiRelease
	if err := json.Unmarshal(body, &data); err != nil {
		return Release{}, errors.New("GitHub API returned malformed release metadata")
	}
	if data.Draft || data.Prerelease {
		return Release{}, errors.New("GitHub API did not return a stable release")
	}
	release := Release{Version: data.TagName, URL: data.HTMLURL, PublishedAt: data.PublishedAt}
	if err := validateRelease(release); err != nil {
		return Release{}, err
	}
	return release, nil
}

func validateRelease(release Release) error {
	if !semver.IsValid(release.Version) || semver.Prerelease(release.Version) != "" || semver.Build(release.Version) != "" {
		return errors.New("official release has an invalid stable SemVer tag")
	}
	if !validReleaseURL(release.URL, release.Version) {
		return errors.New("official release has an unexpected release URL")
	}
	return nil
}

func validReleaseURL(raw, tag string) bool {
	want := "https://github.com/sung2708/DBVault/releases/tag/" + tag
	return raw == want
}

func (c *Checker) Check(ctx context.Context, installed string, force bool) (Result, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	res := Result{InstalledVersion: installed}
	if installed == "" || installed == "dev" || strings.HasPrefix(installed, "(devel)") {
		res.State = Development
		return res, nil
	}
	if !semver.IsValid(installed) {
		res.State = VersionUnknown
	}
	if !force {
		if cached, ok := c.readCache(now()); ok {
			return compare(installed, cached.Release, cached.CheckedAt, true), nil
		}
	}
	lookup := c.Releases
	if lookup == nil {
		lookup = c
	}
	release, err := lookup.Latest(ctx)
	if err != nil {
		res.State = CheckFailed
		res.Reason = err.Error()
		return res, err
	}
	if err := validateRelease(release); err != nil {
		res.State = CheckFailed
		res.Reason = err.Error()
		return res, err
	}
	checked := now()
	c.writeCache(cacheEntry{Release: release, CheckedAt: checked})
	return compare(installed, release, checked, false), nil
}

func compare(installed string, release Release, checked time.Time, cacheHit bool) Result {
	res := Result{InstalledVersion: installed, LatestVersion: release.Version, ReleaseURL: release.URL, CheckedAt: checked, CacheHit: cacheHit}
	if !semver.IsValid(installed) {
		res.State = VersionUnknown
		return res
	}
	cmp := semver.Compare(installed, release.Version)
	switch {
	case cmp < 0:
		res.State = UpdateAvailable
		res.UpdateAvailable = true
		res.UpdateCommand = "go install " + ModulePath + "@" + release.Version
		res.MajorUpgrade = semver.Major(installed) != semver.Major(release.Version) && semver.Major(installed) != "v0"
	case cmp > 0:
		res.State = InstalledNewer
	default:
		res.State = UpToDate
	}
	return res
}

func (c *Checker) readCache(now time.Time) (cacheEntry, bool) {
	path := c.CachePath
	if path == "" {
		path = defaultCachePath()
	}
	if path == "" {
		return cacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxResponse {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if json.Unmarshal(data, &entry) != nil || !validCache(entry, now) {
		return cacheEntry{}, false
	}
	return entry, true
}

func validCache(entry cacheEntry, now time.Time) bool {
	return !entry.CheckedAt.IsZero() && !entry.CheckedAt.After(now) && now.Sub(entry.CheckedAt) < CacheTTL && semver.IsValid(entry.Release.Version) && semver.Prerelease(entry.Release.Version) == "" && semver.Build(entry.Release.Version) == "" && validReleaseURL(entry.Release.URL, entry.Release.Version)
}

func (c *Checker) writeCache(entry cacheEntry) {
	path := c.CachePath
	if path == "" {
		path = defaultCachePath()
	}
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0600)
	if json.NewEncoder(tmp).Encode(entry) != nil {
		_ = tmp.Close()
		return
	}
	if tmp.Sync() != nil {
		_ = tmp.Close()
		return
	}
	if tmp.Close() != nil {
		return
	}
	_ = os.Rename(name, path)
}

func defaultCachePath() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "dbvault", "update-check.json")
}
