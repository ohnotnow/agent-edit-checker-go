package aec

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Version and RepoURL are stamped by ldflags in release builds.
var (
	Version = "dev"
	RepoURL = "https://github.com/ohnotnow/agent-edit-checker-go"
)

// latestReleaseURL is the GitHub API endpoint the update check asks.
// Tests point it at a local server.
var latestReleaseURL = func() string { return buildAPIURL(RepoURL) }

// ghAsset is a single binary attached to a GitHub release.
type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// ghRelease is the slice of the GitHub releases/latest payload used here.
type ghRelease struct {
	TagName string    `json:"tag_name"`
	Body    string    `json:"body"`
	Assets  []ghAsset `json:"assets"`
}

// version prints the running version and, on release builds, whether a
// newer release exists. A failed lookup is not an error.
func version(args []string, _ io.Reader, stdout, _ io.Writer) error {
	if len(args) != 0 {
		return usageErr("version takes no arguments")
	}
	fmt.Fprintf(stdout, "aec version %s\n", Version)
	if Version == "dev" {
		return nil
	}
	latest, err := checkLatestRelease()
	if err != nil {
		return nil
	}
	if isNewer(latest, Version) {
		fmt.Fprintf(stdout, "A newer version (%s) is available.\n", latest)
		fmt.Fprintf(stdout, "Visit %s/releases/latest to update, or run `aec self-update`.\n", RepoURL)
	} else {
		fmt.Fprintln(stdout, "You are running the latest version.")
	}
	return nil
}

// fetchLatestRelease asks the GitHub API for the latest published release.
// The caller picks the client so it can set a fitting timeout.
func fetchLatestRelease(client *http.Client, apiURL string) (*ghRelease, error) {
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// checkLatestRelease returns the latest tag, with a short timeout so an
// unreachable network does not make the command feel hung.
func checkLatestRelease() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	rel, err := fetchLatestRelease(client, latestReleaseURL())
	if err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func buildAPIURL(repoURL string) string {
	path := strings.TrimPrefix(repoURL, "https://github.com/")
	path = strings.TrimPrefix(path, "http://github.com/")
	path = strings.TrimSuffix(path, "/")
	return "https://api.github.com/repos/" + path + "/releases/latest"
}

// isNewer reports whether latest is a higher vX.Y.Z than current. Anything
// that does not parse as three numbers compares as not newer.
func isNewer(latest, current string) bool {
	parse := func(v string) (int, int, int, bool) {
		v = strings.TrimPrefix(v, "v")
		parts := strings.Split(v, ".")
		if len(parts) != 3 {
			return 0, 0, 0, false
		}
		major, err1 := strconv.Atoi(parts[0])
		minor, err2 := strconv.Atoi(parts[1])
		patch, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, 0, 0, false
		}
		return major, minor, patch, true
	}
	lMaj, lMin, lPat, lok := parse(latest)
	cMaj, cMin, cPat, cok := parse(current)
	if !lok || !cok {
		return false
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPat > cPat
}
