// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package selfupdate lets the binary update itself from the project's
// GitHub releases - the scripted form of "re-run the installer".
//
// Trust chain, identical to install.sh: the asset's sha256 must appear
// in the release's checksums.txt, and the freshly extracted binary must
// report its own version before it is swapped in. The current binary is
// kept as <name>.old (on Windows that rename is the only legal way to
// replace a running executable).
//
// Version currency needs its own small comparator: internal/semverrange
// deliberately refuses prerelease bounds (right choice for source refs),
// but harnessator's own releases ARE prereleases (vX.Y.Z-beta).
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the GitHub source of releases; HARNESSATOR_REPO
// overrides it (same convention as install.sh).
const DefaultRepo = "aignerdevelopment/harnessator"

// Client resolves and downloads releases. APIBase/WebBase are swappable
// for tests (and proxies); HARNESSATOR_UPDATE_BASE overrides both.
type Client struct {
	APIBase string
	WebBase string
	HTTP    *http.Client
}

func Default() *Client {
	c := &Client{
		APIBase: "https://api.github.com",
		WebBase: "https://github.com",
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
	if base := os.Getenv("HARNESSATOR_UPDATE_BASE"); base != "" {
		c.APIBase, c.WebBase = base, base
	}
	return c
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "harnessator-selfupdate")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return c.HTTP.Do(req)
}

// Latest resolves the newest release tag. api.github.com first (it is
// authoritative); on ANY API failure it falls back to the plain
// github.com/releases/latest redirect, which is not per-IP rate-limited -
// the same hardening install.sh carries.
func (c *Client) Latest(ctx context.Context, repo string) (string, error) {
	var apiErr error
	resp, err := c.get(ctx, c.APIBase+"/repos/"+repo+"/releases/latest")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var rel struct {
				TagName string `json:"tag_name"`
			}
			if jerr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); jerr == nil && rel.TagName != "" {
				return rel.TagName, nil
			}
			apiErr = fmt.Errorf("release JSON unreadable")
		} else {
			apiErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
	} else {
		apiErr = err
	}
	// redirect fallback: do NOT follow, read Location
	nofollow := *c.HTTP
	nofollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, c.WebBase+"/"+repo+"/releases/latest", nil)
	if rerr != nil {
		return "", rerr
	}
	req.Header.Set("User-Agent", "harnessator-selfupdate")
	resp2, err := nofollow.Do(req)
	if err != nil {
		return "", fmt.Errorf("api failed (%v) and redirect probe failed too (%v)", apiErr, err)
	}
	defer resp2.Body.Close()
	if loc := resp2.Header.Get("Location"); strings.Contains(loc, "/releases/tag/") {
		return loc[strings.LastIndex(loc, "/")+1:], nil
	}
	return "", fmt.Errorf("api failed (%v); releases/latest gave HTTP %d without a tag redirect", apiErr, resp2.StatusCode)
}

// --- version comparison (prerelease-aware, tiny) ----------------------------

type ver struct {
	core [3]int
	pre  []string // empty = release, which outranks any prerelease
}

func parseVer(s string) (ver, error) {
	var v ver
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "v"), "="))
	if s == "" {
		return v, errors.New("empty version")
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata is ignored
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, fmt.Errorf("too many version segments in %q", s)
	}
	for i := range parts {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return v, fmt.Errorf("bad version segment %q", parts[i])
		}
		v.core[i] = n
	}
	return v, nil
}

// cmp: -1 a<b, 0 equal, 1 a>b (semver ordering incl. the prerelease rule).
func cmp(a, b string) (int, error) {
	va, err := parseVer(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseVer(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if va.core[i] != vb.core[i] {
			return sign(va.core[i] - vb.core[i]), nil
		}
	}
	switch {
	case len(va.pre) == 0 && len(vb.pre) == 0:
		return 0, nil
	case len(va.pre) == 0:
		return 1, nil // release beats prerelease
	case len(vb.pre) == 0:
		return -1, nil
	}
	for i := range va.pre {
		if i >= len(vb.pre) {
			return 1, nil // longer set outranks
		}
		if c := cmpPreIdent(va.pre[i], vb.pre[i]); c != 0 {
			return c, nil
		}
	}
	if len(vb.pre) > len(va.pre) {
		return -1, nil
	}
	return 0, nil
}

func cmpPreIdent(a, b string) int {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	switch {
	case ea == nil && eb == nil:
		return sign(na - nb)
	case ea == nil:
		return -1 // numeric < alphanumeric
	case eb == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// Newer reports whether latest supersedes current.
func Newer(current, latest string) (bool, error) {
	c, err := cmp(current, latest)
	if err != nil {
		return false, err
	}
	return c < 0, nil
}

// --- asset plumbing ----------------------------------------------------------

// AssetName is the release artifact for a platform.
func AssetName(tag, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("harnessator_%s_%s_%s.%s", tag, goos, goarch, ext)
}

// parseChecksums maps BASENAME -> hex digest; tolerates the "dist/" path
// prefix and the binary-mode "*" marker that sha256sum emits.
func parseChecksums(text string) map[string]string {
	out := map[string]string{}
	for _, ln := range strings.Split(text, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(f[1], "*"), "")
		out[filepath.Base(name)] = f[0]
	}
	return out
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// extractBinary pulls the harnessator executable out of the downloaded
// archive (tar.gz or zip, entry names may or may not carry "./").
func extractBinary(asset string, blob []byte) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if isBinaryEntry(filepath.Base(f.Name)) {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errors.New("no harnessator binary inside the zip")
	}
	gz, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("no harnessator binary inside the tar.gz")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && isBinaryEntry(filepath.Base(h.Name)) {
			return io.ReadAll(tr)
		}
	}
}

func isBinaryEntry(base string) bool {
	return base != "LICENSE" && strings.Contains(base, "harnessator")
}

// --- the update itself --------------------------------------------------------

type Result struct {
	From, To, Target, Backup string
}

// Update downloads tag's asset for the CURRENT platform, verifies it
// against the release checksums, smoke-runs the fresh binary
// (`<new> version` must report tag) and only then swaps it in. target is
// the path of the running executable.
func (c *Client) Update(ctx context.Context, repo, tag, target string) (*Result, error) {
	asset := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	base := c.WebBase + "/" + repo + "/releases/download/" + tag + "/"

	sumsRaw, err := c.fetch(ctx, base+fmt.Sprintf("harnessator_%s_checksums.txt", tag))
	if err != nil {
		return nil, fmt.Errorf("fetch checksums: %w", err)
	}
	want, ok := parseChecksums(string(sumsRaw))[asset]
	if !ok {
		return nil, fmt.Errorf("%s is not in the release checksums - refusing to install an unlisted artifact", asset)
	}
	blob, err := c.fetch(ctx, base+asset)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", asset, err)
	}
	if got := sha256Hex(blob); got != want {
		return nil, fmt.Errorf("sha256 mismatch for %s (got %s..., want %s...) - refusing", asset, got[:12], want[:12])
	}
	bin, err := extractBinary(asset, blob)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(target)
	tmp := filepath.Join(dir, ".harnessator-new-"+tag)
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		mode = 0o644
	}
	if err := os.WriteFile(tmp, bin, mode); err != nil {
		return nil, err
	}
	// smoke: the fresh binary must introduce itself with the tag we fetched
	out, err := exec.CommandContext(ctx, tmp, "version").Output()
	if err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("downloaded binary failed its 'version' smoke test (%v) - original kept", err)
	}
	if !strings.Contains(string(out), strings.TrimPrefix(tag, "v")) {
		os.Remove(tmp)
		return nil, fmt.Errorf("version smoke test failed: downloaded binary reports %q, expected %s - original kept", strings.TrimSpace(string(out)), tag)
	}

	backup := target + ".old"
	os.Remove(backup) // a stale backup from a prior update must not block the rename
	if err := os.Rename(target, backup); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("cannot move the current binary aside (%v) - permissions? run via an administrator shell, or re-run the installer", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		if rerr := os.Rename(backup, target); rerr != nil {
			return nil, fmt.Errorf("swap failed (%v) AND restore failed (%v): binary is at %s", err, rerr, backup)
		}
		os.Remove(tmp)
		return nil, fmt.Errorf("swap failed (%v) - original restored to %s", err, target)
	}
	return &Result{To: tag, Target: target, Backup: backup}, nil
}

func (c *Client) fetch(ctx context.Context, url string) ([]byte, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty download from %s", url)
	}
	return body, nil
}
