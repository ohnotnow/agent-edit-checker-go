package aec

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// releaseServer serves a fake releases/latest endpoint and points the
// version check at it. It returns a counter of requests received.
func releaseServer(t *testing.T, status int, body string) *int {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	old := latestReleaseURL
	latestReleaseURL = func() string { return srv.URL }
	t.Cleanup(func() { latestReleaseURL = old; srv.Close() })
	return &calls
}

func setVersion(t *testing.T, v string) {
	t.Helper()
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
}

func TestVersionDevBuild(t *testing.T) {
	calls := releaseServer(t, 200, `{"tag_name":"v9.9.9"}`)
	setVersion(t, "dev")
	for _, arg := range []string{"version", "--version"} {
		code, stdout, stderr := runOut(t, "", arg)
		if code != 0 || stdout != "aec version dev\n" || stderr != "" {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", arg, code, stdout, stderr)
		}
	}
	if *calls != 0 {
		t.Errorf("dev build made %d requests", *calls)
	}
}

func TestVersionNewerAvailable(t *testing.T) {
	releaseServer(t, 200, `{"tag_name":"v1.1.0"}`)
	setVersion(t, "v1.0.0")
	code, stdout, _ := runOut(t, "", "version")
	want := "aec version v1.0.0\nA newer version (v1.1.0) is available.\nVisit " + RepoURL + "/releases/latest to update, or run `aec self-update`.\n"
	if code != 0 || stdout != want {
		t.Errorf("code=%d\ngot:  %q\nwant: %q", code, stdout, want)
	}
}

func TestVersionLatest(t *testing.T) {
	releaseServer(t, 200, `{"tag_name":"v1.0.0"}`)
	setVersion(t, "v1.0.0")
	_, stdout, _ := runOut(t, "", "version")
	if stdout != "aec version v1.0.0\nYou are running the latest version.\n" {
		t.Errorf("stdout=%q", stdout)
	}
}

func TestVersionLookupFails(t *testing.T) {
	releaseServer(t, 500, ``)
	setVersion(t, "v1.0.0")
	code, stdout, stderr := runOut(t, "", "version")
	if code != 0 || stdout != "aec version v1.0.0\n" || stderr != "" {
		t.Errorf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.1", "v1.0.0", true},
		{"v1.1.0", "v1.0.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.0", "v1.0.1", false},
		{"1.2.3", "v1.2.2", true},
		{"v1.2", "v1.1.0", false},
		{"v1.x.0", "v1.0.0", false},
		{"v1.0.0", "dev", false},
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestBuildAPIURL(t *testing.T) {
	got := buildAPIURL("https://github.com/owner/repo/")
	if got != "https://api.github.com/repos/owner/repo/releases/latest" {
		t.Errorf("got %q", got)
	}
	if !strings.HasPrefix(buildAPIURL(RepoURL), "https://api.github.com/repos/ohnotnow/agent-edit-checker-go/") {
		t.Errorf("default repo URL: %q", buildAPIURL(RepoURL))
	}
}
