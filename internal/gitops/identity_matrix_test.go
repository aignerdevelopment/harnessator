// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0

package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every URL spelling a redirect-following git can leave behind vs the
// plain browser URL people type - the store identity guard must see
// ONE repository in all of them (the macOS field report: a clone
// whose origin got rewritten to ".git/" by git's redirect handling).
func TestIdentityMatrix(t *testing.T) {
	// the field report's config spelling (with .git)
	cfg := "https://github.com/aignerdevelopment/agentictools.git"
	for _, o := range []string{
		"https://github.com/aignerdevelopment/agentictools.git",
		"https://github.com/aignerdevelopment/agentictools.git/",
		"HTTPS://GitHub.com/aignerdevelopment/agentictools.GIT/",
		"git@github.com:aignerdevelopment/agentictools.git",
		"ssh://git@github.com:22/aignerdevelopment/agentictools",
		"https://github.com/aignerdevelopment/agentictools/.",
		"https://github.com/aignerdevelopment/agentictools.git/./",
		"https://www.github.com/aignerdevelopment/agentictools.git",
		"https://user:pw@github.com/aignerdevelopment/agentictools.git",
		"https://github.com//aignerdevelopment///agentictools.git//",
		// the exact 2026-11-27 macOS field case, verbatim: git rewrote
		// the clone origin to GitHub's canonical www host, the config
		// carries the .git spelling - ONE repo, both directions:
		"https://www.github.com/aignerdevelopment/agentictools",
	} {
		if !SameRepo(o, cfg) {
			t.Errorf("must be ONE repo: %s (identities %q vs %q)", o, RepoIdentity(o), RepoIdentity(cfg))
		}
	}
	// and the guard must still FIRE on real differences
	for _, o := range []string{
		"https://github.com/somebodyelse/agentictools.git",
		"https://gitlab.com/aignerdevelopment/agentictools",
		"https://github.com/aignerdevelopment/other-repo.git",
		"https://github.com/aignerdevelopment/agentictools-v2",
	} {
		if SameRepo(o, cfg) {
			t.Errorf("must be ANOTHER repo: %s -> %q", o, RepoIdentity(o))
		}
	}
}

// End-to-end through ensureClone: a store clone whose origin carries
// the redirect-rewritten spelling must NOT trip the identity guard,
// while a truly different origin must refuse with the self-diagnosing
// block (raw + normalized lines).
func TestEnsureCloneRedirectOrigin(t *testing.T) {
	store := t.TempDir()
	cloneDir := filepath.Join(store, "sources", "acme", "repo")
	mk := func(origin string) {
		os.RemoveAll(cloneDir)
		os.MkdirAll(cloneDir, 0o755)
		run := exec.Command("git", "init", "-q", cloneDir)
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		cmd := exec.Command("git", "-C", cloneDir, "config", "remote.origin.url", origin)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("set origin: %v %s", err, out)
		}
	}
	cfg := "https://github.com/aignerdevelopment/agentictools"

	// rewrite spelling + a fetch we do not want to actually run: the
	// identity check happens BEFORE fetch, so fetch=true still refuses
	// early on mismatch - here we only test the accept/refuse decision
	// via a fetch=false call against a never-contacted remote.
	mk("https://github.com/aignerdevelopment/agentictools.git/")
	if err := ensureClone(t.Context(), cloneDir, "acme", cfg, false); err != nil {
		t.Fatalf("redirect-rewritten origin must pass the guard: %v", err)
	}

	mk("https://github.com/aignerdevelopment/DIFFERENT-repo.git")
	err := ensureClone(t.Context(), cloneDir, "acme", cfg, false)
	if err == nil {
		t.Fatal("different repo must still refuse")
	}
	msg := err.Error()
	for _, want := range []string{"normalized identities", "github.com/aignerdevelopment/agentictools", "source forget acme"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must carry %q, got:\n%s", want, msg)
		}
	}
}
