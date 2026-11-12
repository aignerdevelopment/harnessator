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
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CLI surface only: the heavy lifting (swap, checksums, smoke) is tested
// in internal/selfupdate; here the decision tree and exit codes.
func TestSelfupdateCLIDecisions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer srv.Close()
	t.Setenv("HARNESSATOR_UPDATE_BASE", srv.URL)
	t.Setenv("HARNESSATOR_REPO", "x/r")
	old := Version
	defer func() { Version = old }()

	var out, errOut bytes.Buffer
	Version = "v9.9.9"
	if code := Run(&out, &errOut, []string{"selfupdate", "--check-only"}); code != 0 ||
		!strings.Contains(out.String(), "up to date") {
		t.Fatalf("up-to-date case: code %d\n%s", code, out.String())
	}

	out.Reset()
	Version = "v0.1.0"
	if code := Run(&out, &errOut, []string{"selfupdate", "--check-only"}); code != 1 ||
		!strings.Contains(out.String(), "UPDATE AVAILABLE v0.1.0 -> v9.9.9") {
		t.Fatalf("update-available case: code %d\n%s", code, out.String())
	}

	out.Reset()
	errOut.Reset()
	Version = "dev"
	code := Run(&out, &errOut, []string{"selfupdate"})
	if code != 1 || !strings.Contains(out.String(), "development build") ||
		!strings.Contains(out.String(), "install.sh") {
		t.Fatalf("dev-build refusal: code %d\n%s%s", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Run(&out, &errOut, []string{"selfupdate", "--bogus"}); code != 2 {
		t.Fatalf("bad flag: code %d", code)
	}
}
