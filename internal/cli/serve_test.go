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

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeAPI(t *testing.T) {
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	proj := t.TempDir()
	var out, errOut strings.Builder
	url := mkSourceRepo(t) // file:// local repo from testdata (e2e_test.go)
	if code := Run(&out, &errOut, []string{"init", "--targets", "opencode", "--source", url, "--ref", "~v0.1", proj}); code != 0 {
		t.Fatalf("init: %s%s", out.String(), errOut.String())
	}
	cfg := filepath.Join(proj, "harnessator.toml")
	if code := Run(&out, &errOut, []string{"enable", "--config", cfg, "agentictools-src:minimal"}); code != 0 {
		t.Fatalf("enable: %s%s", out.String(), errOut.String())
	}
	if code := Run(&out, &errOut, []string{"setup", "--config", cfg}); code != 0 {
		t.Fatalf("setup: %s%s", out.String(), errOut.String())
	}

	srv := httptest.NewServer(newServeMux(cfg))
	defer srv.Close()

	// state endpoint reflects the project
	resp, err := http.Get(srv.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st map[string]any
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("state json: %v\n%s", err, body)
	}
	if st["project"] == nil || st["pins"] == nil {
		t.Errorf("state missing fields: %s", body)
	}
	pins, _ := st["pins"].(map[string]any)
	if _, ok := pins["agentictools-src"]; !ok {
		t.Errorf("pin for source missing: %s", body)
	}

	// verify action succeeds over a freshly-locked project
	resp2, err := http.Post(srv.URL+"/api/action", "application/json",
		strings.NewReader(`{"action":"verify"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var act struct {
		OK     bool   `json:"ok"`
		Output string `json:"output"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&act); err != nil {
		t.Fatal(err)
	}
	if !act.OK || !strings.Contains(act.Output, "verified ok") {
		t.Errorf("verify action: %+v", act)
	}

	// the embedded page is served
	page, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	hb, _ := io.ReadAll(page.Body)
	if !strings.Contains(string(hb), "<title>harnessator</title>") {
		t.Error("index.html not served")
	}
}
