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

package host

import (
	"context"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		url     string
		kind    Kind
		org     string
		project string
		name    string
		ok      bool
	}{
		{"https://gitlab.example.com/ai/agentictools.git", GitLab, "", "ai", "agentictools", true},
		{"https://gitlab.example.com/grp/sub/proj.git", GitLab, "", "grp/sub", "proj", true},
		{"git@gitlab.example.com:ai/agentictools.git", GitLab, "", "ai", "agentictools", true},
		{"https://gitlab.com/team/repo", GitLab, "", "team", "repo", true},
		{"https://dev.azure.com/contoso/ProjA/_git/repo1", AzureDevOps, "contoso", "ProjA", "repo1", true},
		{"https://github.com/acme/tools.git", GitHub, "", "acme", "tools", true},
		{"git@github.com:acme/tools.git", GitHub, "", "acme", "tools", true},
		{"github.com/acme/tools", GitHub, "", "acme", "tools", true},
		{"file:///tmp/x/repo", Unknown, "", "", "", false},
		{"/srv/git/repo", Unknown, "", "", "", false},
	}
	for _, tt := range tests {
		r, ok := Parse(tt.url)
		if ok != tt.ok {
			t.Errorf("Parse(%q).ok = %v, want %v", tt.url, ok, tt.ok)
		}
		if r.Kind != tt.kind {
			t.Errorf("Parse(%q).Kind = %v, want %v", tt.url, r.Kind, tt.kind)
		}
		if tt.kind == AzureDevOps {
			if r.Org != tt.org || r.Project != tt.project || r.Name != tt.name {
				t.Errorf("azure parse %q: %+v", tt.url, r)
			}
		}
		if tt.kind == GitLab && r.Name != tt.name {
			t.Errorf("gitlab parse %q: %+v", tt.url, r)
		}
	}
}

func TestMRManualForUnknownHost(t *testing.T) {
	r, _ := Parse("file:///srv/repo") // Unknown kind -> deterministic manual path
	created, msg, err := OpenMR(context.Background(), MROpts{Repo: r, Branch: "feature/x"})
	if created {
		t.Error("Unknown host must not claim an MR was created")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "feature/x") || !strings.Contains(msg, "manually") {
		t.Errorf("manual guidance missing branch/instructions: %q", msg)
	}
}

func TestGitLabParseWeb(t *testing.T) {
	r, _ := Parse("https://gitlab.example.com/ai/agentictools.git")
	if r.Web == "" || !strings.Contains(r.Web, "ai/agentictools") {
		t.Errorf("web base wrong: %q", r.Web)
	}
	// (OpenMR's glab/az/gh CLI paths are exercised in the dev/contribute e2e
	// test via a file:// remote; the deterministic no-CLI fallback shape is
	// covered by TestMRManualForUnknownHost - no network or CLI side
	// effects in unit tests.)
}
