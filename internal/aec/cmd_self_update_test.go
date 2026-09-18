package aec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAssetNameFor(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
	}{
		{"darwin", "arm64", "aec-darwin-arm64"},
		{"darwin", "amd64", "aec-darwin-amd64"},
		{"linux", "amd64", "aec-linux-amd64"},
		{"linux", "arm64", "aec-linux-arm64"},
		{"windows", "amd64", "aec-windows-amd64.exe"},
		{"windows", "arm64", "aec-windows-arm64.exe"},
		{"freebsd", "amd64", ""},
		{"darwin", "386", ""},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"_"+tt.goarch, func(t *testing.T) {
			got := assetNameFor(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("assetNameFor(%q, %q) = %q, want %q",
					tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestLookupChecksum(t *testing.T) {
	sums := strings.Join([]string{
		"abcd1234  aec-darwin-arm64",
		"deadbeef  aec-linux-amd64",
		"feedface *aec-windows-amd64.exe", // text-mode entry
		"",
		"# a comment line that is technically just garbage",
	}, "\n")

	t.Run("binary-mode entry", func(t *testing.T) {
		got, err := lookupChecksum(sums, "aec-darwin-arm64")
		if err != nil {
			t.Fatalf("lookupChecksum: %v", err)
		}
		if got != "abcd1234" {
			t.Errorf("got %q, want abcd1234", got)
		}
	})

	t.Run("text-mode entry with asterisk", func(t *testing.T) {
		got, err := lookupChecksum(sums, "aec-windows-amd64.exe")
		if err != nil {
			t.Fatalf("lookupChecksum: %v", err)
		}
		if got != "feedface" {
			t.Errorf("got %q, want feedface", got)
		}
	})

	t.Run("missing entry", func(t *testing.T) {
		_, err := lookupChecksum(sums, "aec-plan9-amd64")
		if err == nil {
			t.Fatal("expected error for missing entry")
		}
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != exitNotFound {
			t.Errorf("expected not found error, got %T %v", err, err)
		}
	})
}

func TestAtomicSwap(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := atomicSwap(target, []byte("new")); err != nil {
		t.Fatalf("atomicSwap: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after swap: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("contents = %q, want %q", got, "new")
	}

	// No leftover .new-* tempfiles in the directory.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".new-") {
			t.Errorf("leftover tempfile after swap: %s", e.Name())
		}
	}

	if runtime.GOOS != "windows" {
		// Mode preserved as 0755 on POSIX.
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("mode = %o, want 0755", info.Mode().Perm())
		}
	}
}

// fakeRelease stands up an httptest server that mimics enough of the GitHub
// API surface for self-update: the /releases/latest JSON, the binary asset,
// and the SHA256SUMS asset.
type fakeRelease struct {
	srv *httptest.Server
}

func newFakeRelease(t *testing.T, tag, assetName string, assetBytes []byte, badChecksum bool) *fakeRelease {
	t.Helper()
	mux := http.NewServeMux()

	sum := sha256.Sum256(assetBytes)
	hexSum := hex.EncodeToString(sum[:])
	if badChecksum {
		hexSum = strings.Repeat("0", 64)
	}
	sums := fmt.Sprintf("%s  %s\n", hexSum, assetName)

	fr := &fakeRelease{}

	mux.HandleFunc("/asset/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(assetBytes)
	})
	mux.HandleFunc("/asset/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sums))
	})

	fr.srv = httptest.NewServer(mux)

	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		body := struct {
			TagName string    `json:"tag_name"`
			Body    string    `json:"body"`
			Assets  []ghAsset `json:"assets"`
		}{
			TagName: tag,
			Body:    "release notes",
			Assets: []ghAsset{
				{Name: assetName, URL: fr.srv.URL + "/asset/" + assetName},
				{Name: "SHA256SUMS", URL: fr.srv.URL + "/asset/SHA256SUMS"},
			},
		}
		json.NewEncoder(w).Encode(body)
	})

	t.Cleanup(fr.srv.Close)
	return fr
}

func TestRunSelfUpdate_HappyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	newBinary := []byte("new binary content")
	fr := newFakeRelease(t, "v9.9.9", "aec-linux-amd64", newBinary, false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
		assumeYes:      true,
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v\nstdout=%q", err, out.String())
	}

	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, newBinary) {
		t.Errorf("binary not replaced: got %q", got)
	}
	if !strings.Contains(out.String(), "Updated to v9.9.9") {
		t.Errorf("expected confirmation, got %q", out.String())
	}
}

func TestRunSelfUpdate_AlreadyLatest(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	original := []byte("original")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}

	fr := newFakeRelease(t, "v0.1.0", "aec-linux-amd64", []byte("won't be used"), false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}

	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, original) {
		t.Errorf("target was modified despite being up to date: %q", got)
	}
	if !strings.Contains(out.String(), "Already up to date") {
		t.Errorf("expected up-to-date message, got %q", out.String())
	}
}

func TestRunSelfUpdate_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	original := []byte("original")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}

	fr := newFakeRelease(t, "v9.9.9", "aec-linux-amd64", []byte("payload"), true)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
		assumeYes:      true,
	})
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("wrong error: %v", err)
	}

	// Target must remain untouched after a verification failure.
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, original) {
		t.Errorf("target was modified despite checksum failure: %q", got)
	}
}

func TestRunSelfUpdate_UnsupportedPlatform(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", "aec-linux-amd64", []byte("x"), false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     filepath.Join(t.TempDir(), "aec"),
		osName:         "plan9",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err == nil {
		t.Fatal("expected platform error")
	}
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != exitValidation {
		t.Errorf("expected validation error, got %T %v", err, err)
	}
}

func TestRunSelfUpdate_AssetMissing(t *testing.T) {
	// Release exists but doesn't include a Linux build.
	fr := newFakeRelease(t, "v9.9.9", "aec-darwin-arm64", []byte("x"), false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     filepath.Join(t.TempDir(), "aec"),
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err == nil {
		t.Fatal("expected missing-asset error")
	}
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != exitNotFound {
		t.Errorf("expected not found error, got %T %v", err, err)
	}
}

func TestRunSelfUpdate_DevBuildRefuses(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(500)
	}))
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	original := []byte("dev binary")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         srv.URL,
		httpClient:     srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "dev",
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}
	if hits != 0 {
		t.Errorf("dev build made %d HTTP requests; expected 0", hits)
	}
	if !strings.Contains(out.String(), "dev build") {
		t.Errorf("expected dev-build message, got %q", out.String())
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, original) {
		t.Errorf("dev binary was modified: %q", got)
	}
}

func TestRunSelfUpdate_UnwriteableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0555 permissions are not enforced on Windows the same way")
	}

	parent := t.TempDir()
	locked := filepath.Join(parent, "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	target := filepath.Join(locked, "aec")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}

	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		hits++
		body := struct {
			TagName string    `json:"tag_name"`
			Assets  []ghAsset `json:"assets"`
		}{
			TagName: "v9.9.9",
			Assets: []ghAsset{
				{Name: "aec-linux-amd64", URL: "http://example.invalid/should-never-be-hit"},
				{Name: "SHA256SUMS", URL: "http://example.invalid/should-never-be-hit"},
			},
		}
		json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(500)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         srv.URL + "/releases/latest",
		httpClient:     srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err == nil {
		t.Fatal("expected unwriteable error")
	}
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != exitValidation {
		t.Errorf("expected validation error, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "cannot write") {
		t.Errorf("error should mention cannot write: %v", err)
	}
	if hits != 1 {
		t.Errorf("got %d HTTP hits; expected exactly 1 (the release lookup)", hits)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Errorf("target was modified: %q", got)
	}
}

func TestDetectPackageManager(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		goos    string
		gopath  string
		home    string
		wantMgr string
		wantAct pmAction
	}{
		{
			name:    "homebrew on Apple silicon",
			path:    "/opt/homebrew/bin/aec",
			goos:    "darwin",
			wantMgr: "Homebrew",
			wantAct: pmRedirect,
		},
		{
			name:    "homebrew Cellar",
			path:    "/opt/homebrew/Cellar/aec/0.1.0/bin/aec",
			goos:    "darwin",
			wantMgr: "Homebrew",
			wantAct: pmRedirect,
		},
		{
			name:    "linuxbrew",
			path:    "/home/linuxbrew/.linuxbrew/bin/aec",
			goos:    "linux",
			wantMgr: "Homebrew",
			wantAct: pmRedirect,
		},
		{
			name:    "go install with explicit GOPATH",
			path:    "/Users/somebody/code/gopath/bin/aec",
			goos:    "darwin",
			gopath:  "/Users/somebody/code/gopath",
			wantMgr: "'go install'",
			wantAct: pmRedirect,
		},
		{
			name:    "go install with default ~/go/bin",
			path:    "/Users/somebody/go/bin/aec",
			goos:    "darwin",
			home:    "/Users/somebody",
			wantMgr: "'go install'",
			wantAct: pmRedirect,
		},
		{
			name:    "Linux /usr/bin warns",
			path:    "/usr/bin/aec",
			goos:    "linux",
			wantAct: pmWarn,
		},
		{
			name:    "Linux /usr/local/bin warns",
			path:    "/usr/local/bin/aec",
			goos:    "linux",
			wantAct: pmWarn,
		},
		{
			name:    "macOS /usr/local/bin proceeds",
			path:    "/usr/local/bin/aec",
			goos:    "darwin",
			wantAct: pmNone,
		},
		{
			name:    "user-local install proceeds",
			path:    "/Users/somebody/.local/bin/aec",
			goos:    "darwin",
			home:    "/Users/somebody",
			wantAct: pmNone,
		},
		{
			name:    "GOPATH set but binary lives elsewhere",
			path:    "/opt/tools/bin/aec",
			goos:    "linux",
			gopath:  "/home/dev/go",
			wantAct: pmNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint, act := detectPackageManager(tt.path, tt.goos, tt.gopath, tt.home)
			if act != tt.wantAct {
				t.Errorf("action = %v, want %v", act, tt.wantAct)
			}
			if tt.wantMgr != "" && hint.manager != tt.wantMgr {
				t.Errorf("manager = %q, want %q", hint.manager, tt.wantMgr)
			}
		})
	}
}

func TestRunSelfUpdate_PackageManagerRedirect(t *testing.T) {
	target := "/opt/homebrew/bin/aec"

	fr := newFakeRelease(t, "v9.9.9", "aec-darwin-arm64", []byte("would-be-binary"), false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     target,
		osName:         "darwin",
		arch:           "arm64",
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Homebrew") {
		t.Errorf("expected Homebrew mention, got %q", output)
	}
	if !strings.Contains(output, "brew upgrade aec") {
		t.Errorf("expected brew upgrade hint, got %q", output)
	}
}

func TestRunSelfUpdate_CheckUpToDate(t *testing.T) {
	fr := newFakeRelease(t, "v0.1.0", "aec-linux-amd64", []byte("x"), false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     filepath.Join(t.TempDir(), "aec"),
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
		checkOnly:      true,
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("expected up-to-date output, got %q", out.String())
	}
}

func TestRunSelfUpdate_CheckNewerAvailable(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", "aec-linux-amd64", []byte("payload"), false)

	var out bytes.Buffer
	dir := t.TempDir()
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     filepath.Join(dir, "aec"),
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
		checkOnly:      true,
	})
	if err == nil {
		t.Fatal("expected exit-1 signal")
	}
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != 1 || ce.msg != "" {
		t.Errorf("expected silent exit 1, got %T %v", err, err)
	}
	if !strings.Contains(out.String(), "v9.9.9 is available") {
		t.Errorf("expected available-version output, got %q", out.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("expected empty target dir under --check, got %v", entries)
	}
}

func TestRunSelfUpdate_CheckLookupFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         srv.URL,
		httpClient:     srv.Client(),
		targetPath:     filepath.Join(t.TempDir(), "aec"),
		osName:         "linux",
		arch:           "amd64",
		stdout:         &out,
		currentVersion: "v0.1.0",
		checkOnly:      true,
	})
	if err == nil {
		t.Fatal("expected lookup failure")
	}
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != 2 || ce.msg == "" {
		t.Errorf("expected exit 2 with a message, got %T %v", err, err)
	}
}

func TestRunSelfUpdate_PromptYes(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := []byte("new")
	fr := newFakeRelease(t, "v9.9.9", "aec-linux-amd64", newBin, false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdin:          strings.NewReader("y\n"),
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, newBin) {
		t.Errorf("binary not replaced: %q", got)
	}
	if !strings.Contains(out.String(), "Release notes:") {
		t.Errorf("expected release notes in prompt output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Proceed?") {
		t.Errorf("expected proceed prompt, got %q", out.String())
	}
}

func TestRunSelfUpdate_PromptNoAborts(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	original := []byte("old")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}
	fr := newFakeRelease(t, "v9.9.9", "aec-linux-amd64", []byte("new"), false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdin:          strings.NewReader("n\n"),
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, original) {
		t.Errorf("binary modified despite decline: %q", got)
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected aborted output, got %q", out.String())
	}
}

func TestRunSelfUpdate_PromptEmptyInputDeclines(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "aec")
	original := []byte("old")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}
	fr := newFakeRelease(t, "v9.9.9", "aec-linux-amd64", []byte("new"), false)

	var out bytes.Buffer
	err := runSelfUpdate(selfUpdateConfig{
		apiURL:         fr.srv.URL + "/releases/latest",
		httpClient:     fr.srv.Client(),
		targetPath:     target,
		osName:         "linux",
		arch:           "amd64",
		stdin:          strings.NewReader(""),
		stdout:         &out,
		currentVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("runSelfUpdate: %v", err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, original) {
		t.Errorf("binary modified despite empty input: %q", got)
	}
}
