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

package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareMatrix(t *testing.T) {
	cases := []struct {
		current, latest string
		newer           bool
	}{
		{"v0.5.0-beta", "v0.6.0-beta", true},
		{"v0.5.0-beta", "v0.5.0-beta", false},
		{"v0.6.0-beta", "v0.5.0-beta", false},
		{"v0.5.0-beta", "v0.5.0", true}, // release beats its prerelease
		{"v0.5.0", "v0.5.0-beta", false},
		{"v0.5.0", "v0.5.1", true},
		{"v0.9.9", "v0.10.0", true}, // numeric, not lexical
		{"v1.0.0-beta.2", "v1.0.0-beta.10", true},
		{"v1.0.0-alpha", "v1.0.0-beta", true},
		{"v1.0.0-beta", "v1.0.0-beta.1", true}, // longer pre outranks
		{"1.2.3", "v1.2.4", true},              // v prefix tolerated both sides
	}
	for _, c := range cases {
		got, err := Newer(c.current, c.latest)
		if err != nil {
			t.Errorf("Newer(%q,%q): %v", c.current, c.latest, err)
			continue
		}
		if got != c.newer {
			t.Errorf("Newer(%q,%q) = %v, want %v", c.current, c.latest, got, c.newer)
		}
	}
	if _, err := Newer("dev", "v1.0.0"); err == nil {
		t.Error("dev must not compare as a version")
	}
}

func TestParseChecksums(t *testing.T) {
	text := "abc  dist/harnessator_v1_linux_amd64.tar.gz\n" +
		"def  *harnessator_v1_windows_amd64.zip\n" +
		"garbage line\n"
	m := parseChecksums(text)
	if m["harnessator_v1_linux_amd64.tar.gz"] != "abc" {
		t.Errorf("dist/ prefix not stripped: %v", m)
	}
	if m["harnessator_v1_windows_amd64.zip"] != "def" {
		t.Errorf("binary-mode star not handled: %v", m)
	}
}

// fakeRelease serves a release layout: latest resolution, checksums, tar.gz.
// body is the content of the embedded binary; mismatching hash if tamper.
func fakeRelease(t *testing.T, tag, bin string, tamper bool) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "./harnessator_" + tag + "_" + runtime.GOOS + "_" + runtime.GOARCH,
		Mode: 0o755, Size: int64(len(bin))}
	if runtime.GOOS == "windows" {
		hdr.Name += ".exe"
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(bin))
	tw.Close()
	gz.Close()
	asset := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(buf.Bytes())
	line := hex.EncodeToString(sum[:]) + "  dist/" + asset
	if tamper {
		line = strings.Repeat("0", 64) + "  dist/" + asset
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/x/r/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	})
	mux.HandleFunc("/dl/"+tag+"/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dl/"+tag+"/")
		switch {
		case strings.HasSuffix(name, "checksums.txt"):
			fmt.Fprintln(w, line)
		default:
			w.Write(buf.Bytes())
		}
	})
	return httptest.NewServer(mux)
}

func TestLatestFromAPI(t *testing.T) {
	srv := fakeRelease(t, "v9.9.9", "", false)
	defer srv.Close()
	c := &Client{APIBase: srv.URL, WebBase: srv.URL, HTTP: srv.Client()}
	tag, err := c.Latest(context.Background(), "x/r")
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("tag %q err %v", tag, err)
	}
}

func TestLatestRedirectFallback(t *testing.T) {
	// api broken -> the unauthenticated Location redirect saves the day
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			http.Error(w, "rate limit", 403)
			return
		}
		if r.URL.Path == "/x/r/releases/latest" {
			w.Header().Set("Location", "https://github.com/x/r/releases/tag/v7.7.7")
			w.WriteHeader(302)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &Client{APIBase: srv.URL, WebBase: srv.URL, HTTP: srv.Client()}
	tag, err := c.Latest(context.Background(), "x/r")
	if err != nil || tag != "v7.7.7" {
		t.Fatalf("tag %q err %v", tag, err)
	}
}

func TestUpdateHappyPathSwapsAndBacksUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("smoke test embeds a posix script")
	}
	tag := "v9.9.9"
	bin := "#!/bin/sh\necho harnessator " + tag + "\n"
	srv := fakeRelease(t, tag, bin, false)
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "harnessator")
	if err := os.WriteFile(target, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{APIBase: srv.URL, WebBase: srv.URL + "/dl/" + tag, HTTP: srv.Client()}
	// Update fetches checksums/asset relative to WebBase+download path:
	// point WebBase at /dl so the composed URL hits the mux
	res, err := c.Update(context.Background(), "x/r", tag, target)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	raw, _ := os.ReadFile(target)
	if !strings.Contains(string(raw), "harnessator v9.9.9") {
		t.Errorf("target not replaced:\n%s", raw)
	}
	old, err := os.ReadFile(target + ".old")
	if err != nil || string(old) != "OLD BINARY" {
		t.Errorf("backup missing/wrong: %q %v", old, err)
	}
	if res.To != tag || res.Backup != target+".old" {
		t.Errorf("result = %+v", res)
	}
	// temp artifact cleaned up
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".harnessator-new-") {
			t.Errorf("temp binary left behind: %s", e.Name())
		}
	}
}

func TestUpdateRefusesTamperedAndBadSmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix script smoke")
	}
	tag := "v9.9.9"
	// tampered checksums -> refuse before writing anything
	srv := fakeRelease(t, tag, "#!/bin/sh\necho hi\n", true)
	dir := t.TempDir()
	target := filepath.Join(dir, "harnessator")
	os.WriteFile(target, []byte("OLD"), 0o755)
	c := &Client{APIBase: srv.URL, WebBase: srv.URL + "/dl/" + tag, HTTP: srv.Client()}
	_, err := c.Update(context.Background(), "x/r", tag, target)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("want sha256 mismatch, got %v", err)
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "OLD" {
		t.Error("target modified despite mismatch")
	}
	srv.Close()

	// checksums fine, but binary lies about its version -> smoke refuses
	srv2 := fakeRelease(t, tag, "#!/bin/sh\necho harnessator v0.0.1\n", false)
	defer srv2.Close()
	c2 := &Client{APIBase: srv2.URL, WebBase: srv2.URL + "/dl/" + tag, HTTP: srv2.Client()}
	_, err = c2.Update(context.Background(), "x/r", tag, target)
	if err == nil || !strings.Contains(err.Error(), "smoke test") {
		t.Errorf("want smoke failure, got %v", err)
	}
	raw, _ = os.ReadFile(target)
	if string(raw) != "OLD" {
		t.Error("target modified despite smoke failure")
	}
}
