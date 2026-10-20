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

// Package host abstracts the ONLY git-host-specific operations harnessator
// performs: opening a merge/pull request and creating a repository.
// Everything else (branches, commits, pushes) is plain native git.
// When the host's CLI is absent, the fallback
// is a precise, clickable web URL - never a dead end.
//
// GitLab (self-hosted, development tenant) and Azure DevOps (first
// productive tenant) are primary; GitHub support lands with the OSS phase.
package host

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Kind identifies the git hosting platform.
type Kind int

const (
	Unknown Kind = iota
	GitLab
	AzureDevOps
	GitHub
)

func (k Kind) String() string {
	switch k {
	case GitLab:
		return "gitlab"
	case AzureDevOps:
		return "azure-devops"
	case GitHub:
		return "github"
	default:
		return "unknown"
	}
}

// Repo describes a parsed remote URL.
type Repo struct {
	Kind    Kind
	URL     string // original clone URL
	Web     string // browser base, e.g. https://host/group/proj
	Host    string
	Project string // azure project / gitlab namespace(+subgroups)
	Name    string // repo name
	Org     string // azure organization
}

var (
	azureRe   = regexp.MustCompile(`^(?:https://)?dev\.azure\.com/([^/@]+)/([^/_]+)/_git/([^/?#]+)`)
	githubRe  = regexp.MustCompile(`^(?:https://)?(?:www\.)?github\.com/([^/]+)/([^/?#]+?)(?:\.git)?(?:[/?#].*)?$`)
	gitlabSSH = regexp.MustCompile(`^git@([^:]+):(.+?)(?:\.git)?$`)
	httpRe    = regexp.MustCompile(`^(?:https?|ssh|file)://(?:[^@/]+@)?([^/:]+)(?::\d+)?/(.+?)(?:\.git)?/?$`)
)

// Parse identifies the host from a clone URL. file:// and plain paths are
// Unknown but still usable for push (native git handles them).
func Parse(url string) (Repo, bool) {
	if m := azureRe.FindStringSubmatch(url); m != nil {
		return Repo{Kind: AzureDevOps, URL: url,
			Web:  fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s", m[1], m[2], m[3]),
			Host: "dev.azure.com",
			Org:  m[1], Project: m[2], Name: m[3]}, true
	}
	if m := githubRe.FindStringSubmatch(url); m != nil {
		return Repo{Kind: GitHub, URL: url,
			Web:  fmt.Sprintf("https://github.com/%s/%s", m[1], m[2]),
			Host: "github.com", Project: m[1], Name: m[2]}, true
	}
	if m := gitlabSSH.FindStringSubmatch(url); m != nil {
		return normalize(lab(m[1], m[2]), url), true
	}
	if strings.HasPrefix(url, "file://") || strings.HasPrefix(url, "/") {
		return Repo{Kind: Unknown, URL: url}, false
	}
	if m := httpRe.FindStringSubmatch(url); m != nil {
		return normalize(lab(m[1], m[2]), url), true
	}
	return Repo{Kind: Unknown, URL: url}, false
}

// normalize reclassifies generic parses for well-known hosts
// (git@github.com:o/r and https://github.com/o/r arrive via the generic
// ssh/http shapes and must read as GitHub, not GitLab).
func normalize(r Repo, url string) Repo {
	if r.Host == "github.com" {
		r.Kind = GitHub
	}
	return r
}

func lab(host_, path string) Repo {
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	return Repo{Kind: GitLab, URL: "https://" + host_ + "/" + path,
		Web:     "https://" + host_ + "/" + path,
		Host:    host_,
		Project: strings.Join(parts[:len(parts)-1], "/"),
		Name:    parts[len(parts)-1]}
}

// MROpts describes the requested merge request.
type MROpts struct {
	Repo   Repo
	Branch string
	Title  string
	Body   string
	Target string // default branch if empty
}

// OpenMR creates the MR/PR via the host CLI when available, otherwise
// returns a manual web URL. result.Message always explains what happened.
func OpenMR(ctx context.Context, o MROpts) (created bool, message string, err error) {
	if o.Repo.Kind == Unknown {
		return false, fmt.Sprintf(
			"branch %q pushed. Open the merge request manually in your git host UI\n"+
				"(repository: %s)", o.Branch, o.Repo.URL), nil
	}
	switch o.Repo.Kind {
	case GitLab:
		if have("glab") {
			args := []string{"mr", "create", "--source-branch", o.Branch,
				"--title", orDefault(o.Title, "harnessator: update "+o.Repo.Name), "--yes"}
			if o.Target != "" {
				args = append(args, "--target-branch", o.Target)
			}
			if o.Body != "" {
				args = append(args, "--description", o.Body)
			}
			if out, err := exec.CommandContext(ctx, "glab", args...).CombinedOutput(); err != nil {
				return false, "", fmt.Errorf("glab mr create: %v\n%s", err, out)
			}
			return true, "merge request created via glab", nil
		}
		u := fmt.Sprintf("%s/-/merge_requests/new?merge_request[source_branch]=%s", o.Repo.Web, o.Branch)
		return false, "glab not found - create the MR here:\n" + u, nil
	case AzureDevOps:
		if have("az") {
			args := []string{"repos", "pr", "create", "--org", o.Repo.Org,
				"--project", o.Repo.Project, "--repository", o.Repo.Name,
				"--source-branch", o.Branch, "--title", orDefault(o.Title, "harnessator: update "+o.Repo.Name)}
			if o.Target != "" {
				args = append(args, "--target-branch", o.Target)
			}
			if o.Body != "" {
				args = append(args, "--description", o.Body)
			}
			if out, err := exec.CommandContext(ctx, "az", args...).CombinedOutput(); err != nil {
				return false, "", fmt.Errorf("az repos pr create: %v\n%s (is 'az devops login' configured?)", err, out)
			}
			return true, "pull request created via az CLI", nil
		}
		u := fmt.Sprintf("%s/pullrequestcreate?sourceRef=%s", o.Repo.Web, o.Branch)
		if o.Target != "" {
			u += "&targetRef=" + o.Target
		}
		return false, "az CLI not found - create the PR here:\n" + u, nil
	case GitHub:
		if have("gh") {
			args := []string{"pr", "create", "--head", o.Branch,
				"--title", orDefault(o.Title, "harnessator: update "+o.Repo.Name)}
			if o.Body != "" {
				args = append(args, "--body", o.Body)
			}
			if out, err := exec.CommandContext(ctx, "gh", args...).CombinedOutput(); err != nil {
				return false, "", fmt.Errorf("gh pr create: %v\n%s", err, out)
			}
			return true, "pull request created via gh CLI", nil
		}
		u := fmt.Sprintf("%s/compare/%s?expand=1", o.Repo.Web, o.Branch)
		return false, "gh not found - create the PR here:\n" + u, nil
	}
	return false, "no MR automation for this host", nil
}

// CreateRemote creates a new repository via the host CLI (source new).
func CreateRemote(ctx context.Context, kind Kind, name, description string) (string, error) {
	switch kind {
	case GitLab:
		if have("glab") {
			cmd := exec.CommandContext(ctx, "glab", "repo", "create", name,
				"--description", description, "--yes")
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", fmt.Errorf("glab repo create: %v\n%s", err, out)
			}
			return "created via glab (check `glab auth status` for the target group)", nil
		}
	case AzureDevOps:
		if have("az") {
			cmd := exec.CommandContext(ctx, "az", "repos", "create", "--name", name)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", fmt.Errorf("az repos create: %v\n%s", err, out)
			}
			return "created via az CLI", nil
		}
	case GitHub:
		if have("gh") {
			cmd := exec.CommandContext(ctx, "gh", "repo", "create", name, "--description", description)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", fmt.Errorf("gh repo create: %v\n%s", err, out)
			}
			return "created via gh CLI", nil
		}
	}
	return fmt.Sprintf("no %s CLI available - create the repository manually, then add the remote", kind), nil
}

func have(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
