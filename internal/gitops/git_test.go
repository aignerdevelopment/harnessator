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

package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/store"
)

// mkRepo builds a local git repo with tagged releases and an untagged head.
func mkRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "content.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	write("v1\n")
	git("add", ".")
	git("commit", "-qm", "v1")
	git("tag", "v1.0.0")
	write("v2\n")
	git("commit", "-qam", "v2")
	git("tag", "v1.1.0")
	write("unreleased-head\n")
	git("commit", "-qam", "head")
	return dir
}

func TestSyncTagRangePinsLatestTag(t *testing.T) {
	repo := mkRepo(t)
	st := store.Store{Root: t.TempDir()}
	ctx := context.Background()

	pin, err := Sync(ctx, st, config.Source{
		Name: "src", Git: "file://" + repo, Ref: "^v1.0",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Resolved != "v1.1.0" {
		t.Errorf("Resolved = %q, want v1.1.0", pin.Resolved)
	}
	body, err := os.ReadFile(filepath.Join(pin.Dir, "content.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "v2\n" {
		t.Errorf("checkout content = %q, want the tagged v2 tree", body)
	}
	marker, err := os.ReadFile(filepath.Join(pin.Dir, ".harnessator-checkout"))
	if err != nil || strings.TrimSpace(string(marker)) != pin.SHA {
		t.Errorf("marker wrong: %q %v", marker, err)
	}

	// re-Sync reuses the immutable checkout (and does not error)
	pin2, err := Sync(ctx, st, config.Source{Name: "src", Git: "file://" + repo, Ref: "^v1.0"}, true)
	if err != nil || pin2.Dir != pin.Dir || pin2.SHA != pin.SHA {
		t.Errorf("re-sync changed pin: %+v %v", pin2, err)
	}
	// ~v1.0 is [1.0.0, 1.1.0) - node-semver semantics - so it pins v1.0.0
	pin3, err := Sync(ctx, st, config.Source{Name: "src2", Git: "file://" + repo, Ref: "~v1.0"}, true)
	if err != nil || pin3.Resolved != "v1.0.0" {
		t.Errorf("tilde range: %+v %v", pin3, err)
	}
}

func TestSyncBranchRefTracksHead(t *testing.T) {
	repo := mkRepo(t)
	st := store.Store{Root: t.TempDir()}
	pin, err := Sync(context.Background(), st, config.Source{
		Name: "src", Git: "file://" + repo, Ref: "main",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(pin.Dir, "content.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "unreleased-head\n" {
		t.Errorf("branch checkout content = %q", body)
	}
}

func TestSyncUnmatchedRange(t *testing.T) {
	repo := mkRepo(t)
	st := store.Store{Root: t.TempDir()}
	_, err := Sync(context.Background(), st, config.Source{
		Name: "src", Git: "file://" + repo, Ref: "~v9.0",
	}, true)
	if err == nil || !strings.Contains(err.Error(), "matches no tag") {
		t.Fatalf("expected range-miss error, got %v", err)
	}
}
