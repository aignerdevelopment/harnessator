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
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/store"
)

// Dev overlays (isolated per project, native git everywhere): a
// per-source clone on its own branch under
// ~/.harnessator/dev/<source>. Projects rewired to a dev overlay get live
// edits; the read-only pinned checkout keeps every OTHER project intact.

// ErrDirty refuses destructive dev ops on uncommitted work.
var ErrDirty = errors.New("dev overlay has uncommitted changes")

// DevState describes one dev overlay.
type DevState struct {
	Source string
	Dir    string
	Branch string
	Dirty  bool
}

// DevBranchName is a stable, human-meaningful dev branch for the user.
func DevBranchName() string {
	u := "dev"
	if cur, err := user.Current(); err == nil && cur.Username != "" {
		u = strings.NewReplacer(" ", "-", "\\", "-", "/", "-").Replace(cur.Username)
	}
	return "harnessator/dev/" + u
}

// EnsureDev creates (or reuses) the dev overlay clone for src, branched at
// the currently pinned commit. The clone's origin is the REAL remote (cloned
// from the store clone, which carries the upstream URL), so pushes and MR
// flows work without extra plumbing.
func EnsureDev(ctx context.Context, st store.Store, src config.Source, atSHA string, branch string) (*DevState, error) {
	repoDir := filepath.Join(st.SourceDir(src.Name), "repo")
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		return nil, fmt.Errorf("source %s: run setup first to populate the store", src.Name)
	}
	if branch == "" {
		branch = DevBranchName()
	}
	dir := st.DevDir(src.Name)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		cb, err := CurrentBranch(ctx, dir)
		if err != nil {
			return nil, err
		}
		if cb != branch {
			return nil, fmt.Errorf("dev overlay for %s exists on branch %s; asked for %s (drop first?)", src.Name, cb, branch)
		}
		return status(ctx, dir, src.Name, branch)
	}
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%s exists but is not a git overlay; remove it manually", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	if _, err := Run(ctx, "", "clone", "--", repoDir, dir); err != nil {
		return nil, fmt.Errorf("dev clone %s: %w", src.Name, err)
	}
	// branch from the pinned commit, not the clone default
	if _, err := Run(ctx, dir, "checkout", "-b", branch, atSHA); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("dev branch %s at %s: %w", branch, atSHA[:min(12, len(atSHA))], err)
	}
	// origin must be the real remote for pushes (clone from local set it to the
	// store path); rewrite it to the configured URL.
	if _, err := Run(ctx, dir, "remote", "set-url", "origin", src.Git); err != nil {
		return nil, err
	}
	return status(ctx, dir, src.Name, branch)
}

// CurrentBranch returns the checked-out branch of dir.
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(string(out)), err
}

// SyncDev rebases the dev overlay onto the source's current upstream branch.
func SyncDev(ctx context.Context, dir, upstream string) error {
	if upstream == "" {
		return errors.New("no upstream branch to sync against")
	}
	if _, err := Run(ctx, dir, "fetch", "origin"); err != nil {
		return err
	}
	_, err := Run(ctx, dir, "rebase", "origin/"+upstream)
	if err != nil {
		return fmt.Errorf("rebase onto origin/%s failed (resolve in %s, then dev sync again): %w", upstream, dir, err)
	}
	return nil
}

// Status reports dirtiness of the overlay.
func Status(ctx context.Context, st store.Store, srcName, branch string) (*DevState, error) {
	return status(ctx, st.DevDir(srcName), srcName, branch)
}

func status(ctx context.Context, dir, srcName, branch string) (*DevState, error) {
	out, err := Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	return &DevState{
		Source: srcName, Dir: dir, Branch: branch,
		Dirty: strings.TrimSpace(string(out)) != "",
	}, nil
}

// Drop removes the overlay (refusing dirty states unless force).
func Drop(ctx context.Context, st store.Store, srcName, branch string, force bool) error {
	d, err := Status(ctx, st, srcName, branch)
	if err != nil {
		// missing dir etc: nothing to drop
		if _, serr := os.Stat(st.DevDir(srcName)); os.IsNotExist(serr) {
			return nil
		}
		return err
	}
	if d.Dirty && !force {
		return fmt.Errorf("%w in %s - commit/contribute or use --force", ErrDirty, d.Dir)
	}
	if !force {
		// unpushed commits are also "work in progress"
		out, err := Run(ctx, d.Dir, "log", "--branches", "--not", "--remotes", "--oneline")
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return fmt.Errorf("dev overlay %s has unpushed commits - contribute first or use --force", d.Dir)
		}
	}
	return os.RemoveAll(d.Dir)
}

// PushBranch pushes the overlay branch to the real origin (native git).
func PushBranch(ctx context.Context, dir, branch string) error {
	_, err := Run(ctx, dir, "push", "-u", "origin", branch)
	return err
}

// HeadCommit of a directory's checkout.
func HeadCommit(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(string(out)), err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
