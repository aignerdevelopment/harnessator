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

package harvest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	cmd := exec.Command("git", full...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestAdoptProject(t *testing.T) {
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	base := t.TempDir()
	t.Chdir(base)

	donor := filepath.Join(base, "proj")
	write(t, filepath.Join(donor, ".claude/skills/demo/SKILL.md"), "---\nname: demo\ndescription: D.\n---\nbody\n")
	write(t, filepath.Join(donor, ".claude/commands/review.md"), "# Review\ncheck\n")

	res, err := Harvest(Options{Paths: []string{donor}, Name: "pub", Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed {
		t.Error("engine must commit the published source for adopt to clone it")
	}
	if len(res.Adopted) != 1 {
		t.Fatalf("adopted = %+v", res.Adopted)
	}
	ar := res.Adopted[0]
	if ar.Skipped != "" || ar.Linked != 2 || len(ar.Kept) != 0 {
		t.Errorf("adopt report = %+v", ar)
	}

	// donor is now a managed consumer
	if _, err := os.Stat(filepath.Join(donor, "harnessator.lock")); err != nil {
		t.Errorf("donor lock missing: %v", err)
	}
	db, err := os.ReadFile(filepath.Join(donor, "harnessator.toml"))
	if err != nil {
		t.Fatal(err)
	}
	d := string(db)
	if !strings.Contains(d, `git = "file://`) || !strings.Contains(d, "demo") || !strings.Contains(d, "review") {
		t.Errorf("donor config wrong:\n%s", d)
	}
	// originals became symlinks into the store
	lnk, err := os.Readlink(filepath.Join(donor, ".claude/skills/demo"))
	if err != nil || !strings.Contains(lnk, "/checkouts/") {
		t.Errorf("skill not linked: %q %v", lnk, err)
	}
	lnk, err = os.Readlink(filepath.Join(donor, ".claude/commands/review.md"))
	if err != nil {
		t.Errorf("prompt not linked: %v", err)
	}
	// donor state must verify
	if _, err := os.Stat(filepath.Join(donor, ".harnessator/active.md")); err != nil {
		t.Errorf("donor active index missing: %v", err)
	}
}

func TestAdoptKeepsDivergentOriginal(t *testing.T) {
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	base := t.TempDir()
	t.Chdir(base)

	a := filepath.Join(base, "projA")
	b := filepath.Join(base, "projB")
	write(t, filepath.Join(a, ".claude/skills/tool/SKILL.md"), "---\nname: tool\ndescription: T.\n---\nlocal edit A\n")
	write(t, filepath.Join(b, ".claude/skills/tool/SKILL.md"), "---\nname: tool\ndescription: T.\n---\nlocal edit B\n")
	// donor B is unambiguously the newest (dir mtime is what prefer uses)
	recent := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(b, ".claude/skills/tool"), recent, recent); err != nil {
		t.Fatal(err)
	}

	res, err := Harvest(Options{Paths: []string{a, b}, Name: "pub", Prefer: "newest", Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adopted) != 2 {
		t.Fatalf("adopted = %+v", res.Adopted)
	}
	// roots sort deterministically: [0]=projA (divergent -> kept), [1]=projB (match -> linked)
	if got := res.Adopted[0]; got.Linked != 0 || len(got.Kept) != 1 {
		t.Errorf("donor A should keep its divergent original, got %+v", got)
	}
	if got := res.Adopted[1]; got.Linked != 1 || len(got.Kept) != 0 {
		t.Errorf("donor B (published source) should link cleanly, got %+v", got)
	}
	// donor A original must still be a real directory
	fi, err := os.Lstat(filepath.Join(a, ".claude/skills/tool"))
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("divergent original was removed: %v", err)
	}
	// donor B got the managed link
	if _, err := os.Readlink(filepath.Join(b, ".claude/skills/tool")); err != nil {
		t.Errorf("matching original was not linked: %v", err)
	}
}

func TestAdoptSkipsLoosePile(t *testing.T) {
	t.Setenv("HARNESSATOR_HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	base := t.TempDir()
	t.Chdir(base)
	pile := filepath.Join(base, "pile")
	write(t, filepath.Join(pile, "one/SKILL.md"), "---\nname: one\ndescription: O.\n---\n")

	res, err := Harvest(Options{Paths: []string{pile}, Name: "pub", Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adopted) != 1 || res.Adopted[0].Skipped == "" {
		t.Fatalf("pile must be skipped with reason: %+v", res.Adopted)
	}
	// the pile dir itself must be untouched
	if _, err := os.Stat(filepath.Join(pile, "one/SKILL.md")); err != nil {
		t.Error("pile content must never be modified")
	}
}
