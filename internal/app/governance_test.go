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

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/resolve"
)

func copyInto(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func newGovApp(t *testing.T, checkout string) *App {
	t.Helper()
	m, err := config.LoadSourceManifest(filepath.Join(checkout, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		Cfg: &config.ProjectConfig{
			Kind:      config.KindProject,
			Schema:    1,
			Targets:   []string{"opencode"},
			Templates: []string{"agentictools:full"},
			Sources:   []config.Source{{Name: "agentictools", Git: "x", Ref: "main"}},
			Provider:  &config.ProviderConfig{Profile: "agentictools:provider/litellm-demo"},
			Policy:    &config.PolicyConfig{Profiles: []string{"agentictools:policy/models-strict"}},
		},
		Pins: map[string]*gitops.Pinned{"agentictools": {
			Name: "agentictools", Git: "x", Range: "main", Resolved: "main",
			SHA: "1111222233334444555566667777888899990000", Dir: checkout,
		}},
		Manifests: map[string]*config.SourceManifest{m.Package.Name: m},
	}
	var err2 error
	a.Plan, err2 = resolve.Resolve(a.Cfg, a.Manifests)
	if err2 != nil {
		t.Fatal(err2)
	}
	return a
}

func fixtureCheckout(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sources", "agentictools"))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "agentictools")
	copyInto(t, src, dst)
	return dst
}

func TestGovernanceCompliant(t *testing.T) {
	a := newGovApp(t, fixtureCheckout(t))
	if err := a.loadGovernance(); err != nil {
		t.Fatalf("fixture policy is compliant; got %v", err)
	}
	if a.GovProvider == nil || a.GovProvider.Name != "litellm-demo" {
		t.Errorf("provider not loaded: %+v", a.GovProvider)
	}
	if !a.GovPolicy.Strict || len(a.GovPolicy.Sources) != 1 {
		t.Errorf("policy merge wrong: %+v", a.GovPolicy)
	}
}

func TestStrictViolationFails(t *testing.T) {
	checkout := fixtureCheckout(t)
	os.WriteFile(filepath.Join(checkout, "policies", "models-strict.toml"),
		[]byte("strict = true\nallowed = [\"fast\"]\n"), 0o644)
	a := newGovApp(t, checkout)
	err := a.loadGovernance()
	if err == nil || !strings.Contains(err.Error(), "strict") {
		t.Fatalf("expected strict violation error, got %v", err)
	}
	// default_model big is outside allowed fast => violation mentions it
	if !strings.Contains(err.Error(), "default_model") {
		t.Errorf("violation detail missing: %v", err)
	}
}

func TestWarnViolationDoesNotFail(t *testing.T) {
	checkout := fixtureCheckout(t)
	os.WriteFile(filepath.Join(checkout, "policies", "models-strict.toml"),
		[]byte("strict = false\nallowed = [\"fast\"]\n"), 0o644)
	a := newGovApp(t, checkout)
	if err := a.loadGovernance(); err != nil {
		t.Fatalf("non-strict must not fail Open: %v", err)
	}
	if len(a.GovWarnings) == 0 {
		t.Error("expected warnings for non-strict violations")
	}
}

func TestProviderMustBeEnabled(t *testing.T) {
	checkout := fixtureCheckout(t)
	a := newGovApp(t, checkout)
	// reference a provider nobody selected via template/select
	a.Cfg.Provider.Profile = "agentictools:provider/ghost"
	if err := a.loadGovernance(); err == nil ||
		!strings.Contains(err.Error(), "provider.profile") {
		t.Fatalf("expected provider.profile error, got %v", err)
	}
}
