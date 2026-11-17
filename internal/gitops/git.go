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

// Package gitops resolves configured sources into pinned, read-only trees
// inside the store, driving the *system* git binary - as much normal,
// native git as possible; go-git rejected for auth/worktree gaps.
//
// Install-mode checkouts are exported with `git archive` (no .git, no
// branch state - detached and immutable in spirit) into
// sources/<name>/checkouts/<sha>; the digest check in verify/doctor is what
// turns accidental edits into loud errors.
package gitops

import (
	"path"

	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/semverrange"
	"harnessator.org/harnessator/internal/store"
)

// Run executes git in dir ("" = no specific dir) and returns stdout.
func Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Pinned is a source resolved to an exact commit and exported to disk.
type Pinned struct {
	Name     string
	Git      string
	Range    string
	Resolved string // tag name, or branch name for branch refs
	SHA      string
	Dir      string // exported, read-only tree root (for pointer
	// sources: the CONTENT tree - components resolve against it)
	// ManifestRoot is the directory holding the source manifest (the
	// checkout root, or a sub-directory for multi-source repos).
	// The pointer fields are zero for normal sources; a pointer source
	// serves its manifest from this repository and its content bytes
	// from ContentGit at ContentSHA - import-only upstream, both pins
	// in the lock, one honest provenance.
	ContentGit      string
	ContentRange    string
	ContentResolved string
	ContentSHA      string
	ManifestRoot    string
	// ManifestTree is the exported MANIFEST checkout of a pointer source;
	// Dir is the CONTENT tree (where the bytes live). For normal sources
	// the two are one tree and the field stays empty - the split exists
	// so lock selectors can stay manifest-relative and never become a
	// cross-tree ../ chain.
	ManifestTree string
}

// ManifestFile is the manifest path within the checkout ("harnessator.toml"
// or "source/harnessator.toml"); pointer sources delegate to the
// manifest-tree comparison - see ManifestRelative.
func (p *Pinned) ManifestFile() string { return p.ManifestRelative() }

// ManifestRelative is the manifest selector RELATIVE TO THE MANIFEST
// TREE - the sane lock value, and never a cross-tree ../ chain.
func (p *Pinned) ManifestRelative() string {
	if p.ManifestRoot == "" || p.ManifestRoot == p.manifestTreeDir() {
		return "harnessator.toml"
	}
	rel, err := filepath.Rel(p.manifestTreeDir(), filepath.Join(p.ManifestRoot, "harnessator.toml"))
	if err != nil {
		return "harnessator.toml"
	}
	return filepath.ToSlash(rel)
}

func (p *Pinned) manifestTreeDir() string {
	if p.ManifestTree != "" {
		return p.ManifestTree
	}
	return p.Dir
}

// ManifestDir is the directory holding this source's manifest within the
// checkout (the checkout root for single-source repos).
func (p *Pinned) ManifestDir() string {
	if p.ManifestRoot != "" {
		return p.ManifestRoot
	}
	return p.Dir
}

// ManifestDirOf resolves a manifest selector for a checkout directory
// (exported for lock-based pin reconstruction).
func ManifestDirOf(checkoutDir, manifest string) string {
	return manifestRoot(checkoutDir, manifest)
}

// Sync brings the manifest clone up to date, resolves the ref to a SHA
// and exports the checkout. fetch=false reuses the existing clone
// (offline runs). A pointer manifest's [content] is NOT followed here -
// the assembly (app) decides by peeking the loaded manifest and calling
// SyncContent next; Sync stays single-repo by contract.
func Sync(ctx context.Context, st store.Store, src config.Source, fetch bool) (*Pinned, error) {
	repoDir := filepath.Join(st.SourceDir(src.Name), "repo")
	if err := ensureClone(ctx, repoDir, src.Name, src.Git, fetch); err != nil {
		return nil, err
	}
	resolved, sha, err := resolveRef(ctx, repoDir, "source "+src.Name, src.Ref)
	if err != nil {
		return nil, err
	}
	p := &Pinned{Name: src.Name, Git: src.Git, Range: src.Ref, Resolved: resolved, SHA: sha}
	p.Dir = st.CheckoutDir(src.Name, p.SHA)
	p.ManifestRoot = manifestRoot(p.Dir, src.Manifest)
	if err := exportCheckout(ctx, repoDir, p.SHA, p.Dir); err != nil {
		return nil, err
	}
	return p, nil
}

// SyncContent fetches and resolves the CONTENT repository of a pointer
// source. The clone lives in a namespace of its own under the borrowing
// name (sources/<name>/content), so the origin identity guard works the
// same way for both halves: re-pointing a borrowed content clone to a
// third repository is refused exactly like a manifest collision.
func SyncContent(ctx context.Context, st store.Store, name, contentDir string, c config.Content, fetch bool) (resolved, sha, dir string, err error) {
	if c.Git == "" {
		return "", "", "", fmt.Errorf("content of %s: empty git URL", name)
	}
	ref := c.Ref
	if ref == "" {
		ref = "main" // upstream branch names are not ours to assume
	}
	repoDir := filepath.Join(st.ContentDir(name), "repo")
	if err := ensureClone(ctx, repoDir, "content of "+name, c.Git, fetch); err != nil {
		return "", "", "", err
	}
	resolved, sha, err = resolveRef(ctx, repoDir, "content of "+name, ref)
	if err != nil {
		return "", "", "", err
	}
	dir = contentDir
	if dir == "" {
		dir = st.ContentCheckoutDir(name, sha)
	}
	if err := exportCheckout(ctx, repoDir, sha, dir); err != nil {
		return "", "", "", err
	}
	return resolved, sha, dir, nil
}

// DefaultBranch asks a remote for its HEAD symref - the discovery-time
// answer to "what is this repo's main branch called". Falls back to
// "main"; a failed question is answered honestly by the fetch that
// follows, not by guessing twice.
func DefaultBranch(ctx context.Context, gitURL string) string {
	out, err := Run(ctx, "", "ls-remote", "--symref", gitURL, "HEAD")
	if err != nil {
		return "main"
	}
	for _, ln := range strings.Split(string(out), "\n") {
		fields := strings.Fields(ln)
		if len(fields) >= 2 && fields[0] == "ref:" {
			// "ref: refs/heads/main<TAB>HEAD" - the symref target wins
			if i := strings.LastIndex(fields[1], "/"); i >= 0 {
				return fields[1][i+1:]
			}
		}
	}
	return "main"
}

// ensureClone clones gitURL into cloneDir when absent; an existing clone
// must ORIGINALLY track it (identity guard), and fetch=true refreshes it
// as a pure mirror (--force: moved tags must win over cached objects,
// the pre-release re-snapshot practice).
func ensureClone(ctx context.Context, cloneDir, label, gitURL string, fetch bool) error {
	if _, err := os.Stat(filepath.Join(cloneDir, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(cloneDir), 0o755); err != nil {
			return err
		}
		if _, err := Run(ctx, "", "clone", "--", gitURL, cloneDir); err != nil {
			return fmt.Errorf("clone %s: %w", label, err)
		}
		return nil
	}
	if origin, oerr := Origin(cloneDir); oerr == nil && !SameRepo(origin, gitURL) {
		return fmt.Errorf("source %s: one store name, two repositories - the store clone tracks\n    %s\nbut this config points to\n    %s\nharnessator never redirects a shared name silently (another project may own this clone).\n  a) genuinely different repo: give it its own name -\n     harnessator source add \"%s\" --name <distinct>   (and remove %s first)\n  b) this repo really moved: hand the name over -\n     harnessator source forget %s && harnessator setup\n     (projects still pinning the old origin will re-fetch or rename there)",
			label, origin, gitURL, gitURL, label, label)
	}
	if fetch {
		if _, err := Run(ctx, cloneDir, "-c", "gc.auto=0", "fetch", "--tags", "--prune", "--force", "origin"); err != nil {
			return fmt.Errorf("fetch %s: %w", label, err)
		}
	}
	return nil
}

// resolveRef maps one ref expression (branch, tag or semver range) to a
// resolved name and exact commit inside the clone.
func resolveRef(ctx context.Context, repoDir, label, ref string) (string, string, error) {
	rng, err := semverrange.Parse(ref)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", label, err)
	}
	switch {
	case rng.IsBranch():
		out, err := Run(ctx, repoDir, "rev-parse", "refs/remotes/origin/"+ref)
		if err != nil {
			// local clones from file:// may only carry the default branch
			// as a remote ref of a different name; try plain rev-parse.
			out, err = Run(ctx, repoDir, "rev-parse", ref)
			if err != nil {
				return "", "", fmt.Errorf("%s: branch %q not found: %w", label, ref, err)
			}
		}
		return ref, strings.TrimSpace(string(out)), nil
	default:
		tags, err := listTags(ctx, repoDir)
		if err != nil {
			return "", "", err
		}
		tag, ok := rng.Latest(tags)
		if !ok {
			return "", "", fmt.Errorf("%s: ref %q matches no tag (found %d tags)", label, ref, len(tags))
		}
		out, err := Run(ctx, repoDir, "rev-parse", tag+"^{commit}")
		if err != nil {
			return "", "", fmt.Errorf("%s: rev-parse %s: %w", label, tag, err)
		}
		return tag, strings.TrimSpace(string(out)), nil
	}
}

// Origin reports the clone's remote origin URL.
func Origin(repoDir string) (string, error) {
	out, err := Run(context.Background(), repoDir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RepoIdentity normalizes a git URL for identity comparison: transport
// decoration (https vs ssh vs scp-form, trailing .git, default ports,
// file:// vs bare path) does not make it "another repo" - a different
// host, path or local directory does. That distinction is exactly the
// store-name collision we refuse.
func RepoIdentity(u string) string {
	s := strings.TrimSpace(u)
	s = strings.TrimRight(s, "/")
	if len(s) >= 4 && strings.EqualFold(s[len(s)-4:], ".git") {
		s = s[:len(s)-4]
	}
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "file://"):
		p := strings.TrimPrefix(s, "file://")
		p = strings.TrimPrefix(p, "localhost")
		if abs, err := filepath.Abs(p); err == nil {
			return "file:" + abs
		}
		return "file:" + p
	case strings.Contains(low, "://"):
		if parsed, err := url.Parse(s); err == nil && parsed.Host != "" {
			host := strings.ToLower(parsed.Hostname())
			if pt := parsed.Port(); pt != "" && pt != defaultPort(parsed.Scheme) {
				host += ":" + pt
			}
			return host + "/" + strings.Trim(parsed.EscapedPath(), "/")
		}
		return low
	case len(s) >= 2 && s[1] == ':': // windows drive path before scp-form
		if abs, err := filepath.Abs(s); err == nil {
			return "file:" + abs
		}
		return low
	case strings.Contains(s, ":"): // scp-style [user@]host:path
		i := strings.Index(s, ":")
		host, p := s[:i], s[i+1:]
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
		return strings.ToLower(host) + "/" + strings.Trim(p, "/")
	default: // bare local path
		if abs, err := filepath.Abs(s); err == nil {
			return "file:" + abs
		}
		return low
	}
}

func defaultPort(scheme string) string {
	switch strings.ToLower(scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	case "ssh", "git":
		return "22"
	}
	return ""
}

// SameRepo compares two URLs by normalized identity.
func SameRepo(a, b string) bool { return RepoIdentity(a) == RepoIdentity(b) }

// manifestRoot resolves a source manifest selector ("source/harnessator",
// "source/harnessator/harnessator.toml", "harnessator.toml" or empty) to
// the manifest's DIRECTORY inside a checkout.
func manifestRoot(checkoutDir, manifest string) string {
	m := strings.TrimSpace(filepath.ToSlash(manifest))
	if m == "" {
		return checkoutDir
	}
	m = strings.TrimPrefix(m, "./")
	if path.Base(m) == "harnessator.toml" { // bare or nested file path
		m = path.Dir(m)
	}
	m = strings.TrimSuffix(m, "/")
	if m == "" || m == "." || m == "/" {
		return checkoutDir
	}
	return filepath.Join(checkoutDir, filepath.FromSlash(m))
}

func listTags(ctx context.Context, repoDir string) ([]string, error) {
	out, err := Run(ctx, repoDir, "tag", "--list")
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, t := range strings.Fields(string(out)) {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags, nil
}

// exportCheckout writes the tree at sha into dir via git archive (no .git).
// Existing complete checkouts are reused untouched (pins are immutable).
func exportCheckout(ctx context.Context, repoDir, sha, dir string) error {
	marker := filepath.Join(dir, ".harnessator-checkout")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	tmp := dir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}

	archive := exec.CommandContext(ctx, "git", "-C", repoDir, "archive", "--format=tar", sha)
	tarReader, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	if err := archive.Start(); err != nil {
		return fmt.Errorf("git archive %s: %w", sha, err)
	}
	if err := untar(tmp, tarReader); err != nil {
		_ = archive.Wait()
		os.RemoveAll(tmp)
		return fmt.Errorf("export %s: %w", sha, err)
	}
	if err := archive.Wait(); err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("git archive %s: %w", sha, err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".harnessator-checkout"), []byte(sha+"\n"), 0o444); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := makeReadOnly(tmp); err != nil {
		// not fatal on odd filesystems; verify/doctor still detect edits
		fmt.Fprintf(os.Stderr, "warn: could not mark %s read-only: %v\n", tmp, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// untar extracts a git-archive stream into root, rejecting escaping paths.
func untar(root string, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(h.Name)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe archive path %q", h.Name)
		}
		target := filepath.Join(root, clean)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(h.Mode & 0o777)
			if mode == 0 {
				mode = 0o644
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		default:
			// hardlinks/devices in source repos: skip loudly later if ever
			// needed; git archives of normal projects are file/dir/symlink.
			continue
		}
	}
}

func makeReadOnly(dir string) error {
	// Files become read-only; directories keep their write bit so that
	// re-exports (RemoveAll) still work. Drift detection is digest-based
	// (verify/doctor), not permission-based.
	return filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return os.Chmod(p, 0o444)
	})
}
