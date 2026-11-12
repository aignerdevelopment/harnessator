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

// Provenance and attribution for harvested components. Two invariants a
// published harvest must never break:
//
//   - no machine-local absolute path enters the manifest (the component
//     origin labels donors upstream-first: git remote URL + commit when
//     the donor lives in a repository, else the donor directory name);
//   - redistributed content carries the donor's license/attribution
//     files, because MIT-style licenses survive copying only when their
//     text travels with the copy.
//
// The license sniff only names what the text proves (a handful of common
// shapes); anything else leaves the package field unset rather than
// guessing.

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"harnessator.org/harnessator/internal/gitops"
)

type donorMeta struct {
	label    string   // "url@sha" | "topdir@sha" | "basename"
	toplevel string   // git top-level directory when the donor is in a repo
	homepage string   // sanitized remote URL ("" without an https/ssh remote)
	licID    string   // sniffed from the license text ("" when unknown)
	licPaths []string // donor-side license + notice files to carry
}

// Candidate names, lowercased; first license match per donor wins,
// notices always join the carry list.
var licenseNames = []string{"license", "license.md", "license.txt", "unlicense", "copying", "copying.md", "copying.txt"}
var noticeNames = []string{"notice", "notice.md", "notice.txt"}

func probeDonor(ctx context.Context, root string) donorMeta {
	d := donorMeta{label: filepath.Base(root)}
	if out, err := gitops.Run(ctx, root, "rev-parse", "--show-toplevel"); err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			d.toplevel = top
			d.label = filepath.Base(top)
			if uo, err := gitops.Run(ctx, top, "remote", "get-url", "origin"); err == nil {
				d.homepage = sanitizeRemote(strings.TrimSpace(string(uo)))
			}
			if so, err := gitops.Run(ctx, top, "rev-parse", "--short=12", "HEAD"); err == nil {
				if sha := strings.TrimSpace(string(so)); sha != "" {
					if d.homepage != "" {
						d.label = d.homepage + "@" + sha
					} else {
						d.label = filepath.Base(top) + "@" + sha
					}
				}
			}
		}
	}
	dir := root
	if d.toplevel != "" {
		dir = d.toplevel
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return d
	}
	found := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			found[strings.ToLower(e.Name())] = e.Name()
		}
	}
	for _, cand := range licenseNames {
		if name, ok := found[cand]; ok {
			p := filepath.Join(dir, name)
			if data, err := os.ReadFile(p); err == nil {
				d.licID = sniffLicense(data)
				d.licPaths = append(d.licPaths, p)
			}
			break
		}
	}
	for _, cand := range noticeNames {
		if name, ok := found[cand]; ok {
			d.licPaths = append(d.licPaths, filepath.Join(dir, name))
		}
	}
	return d
}

// fragment labels one donor-side location for the manifest origin string.
func (d donorMeta) fragment(root, locAbs string) string {
	base := root
	if d.toplevel != "" {
		base = d.toplevel
	}
	rel, err := filepath.Rel(base, locAbs)
	if err != nil {
		rel = filepath.Base(locAbs)
	}
	return d.label + ":" + filepath.ToSlash(rel)
}

// sanitizeRemote strips embedded credentials and refuses labels that
// would re-introduce a local path (file:// or bare-path remotes).
func sanitizeRemote(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		// scp-style ssh remotes (git@host:path) carry no local path.
		if strings.Contains(u, "@") && strings.Contains(u, ":") {
			return strings.TrimSuffix(u, ".git")
		}
		return ""
	}
	if strings.HasPrefix(u, "file://") {
		return ""
	}
	scheme, rest := u[:i+3], u[i+3:]
	end := strings.Index(rest, "/")
	if end < 0 {
		end = len(rest)
	}
	auth, tail := rest[:end], rest[end:]
	if at := strings.LastIndex(auth, "@"); at >= 0 {
		auth = auth[at+1:]
	}
	if auth == "" {
		return ""
	}
	return strings.TrimSuffix(scheme+auth+tail, ".git")
}

// sniffLicense names only the shapes it can prove from the text.
func sniffLicense(data []byte) string {
	s := string(data)
	switch {
	case strings.Contains(s, "MIT License"),
		strings.Contains(s, "Permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(s, "Apache License"):
		if strings.Contains(s, "Version 2.0") {
			return "Apache-2.0"
		}
		return ""
	case strings.Contains(s, "Permission to use, copy, modify, and/or distribute"):
		return "ISC"
	case strings.Contains(s, "Redistribution and use in source and binary forms"):
		if strings.Contains(s, "Neither the name") {
			return "BSD-3-Clause"
		}
		return ""
	case strings.Contains(s, "free and unencumbered"):
		return "Unlicense"
	}
	return ""
}

// carryLicenses copies donor license/notice files into the harvest
// output; an existing file of the same name always governs (a curated
// source is never overwritten by a donor). Returns the copied names and
// the first provable license id of the donors.
func carryLicenses(absOut string, donors []donorMeta) ([]string, string, error) {
	var carried []string
	seen := map[string]bool{}
	for _, d := range donors {
		for _, src := range d.licPaths {
			name := filepath.Base(src)
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			dst := filepath.Join(absOut, name)
			if _, err := os.Stat(dst); err == nil {
				continue // output governs its own attribution
			}
			data, err := os.ReadFile(src)
			if err != nil {
				return nil, "", err
			}
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return nil, "", err
			}
			carried = append(carried, name)
		}
	}
	for _, d := range donors {
		if d.licID != "" {
			return carried, d.licID, nil
		}
	}
	return carried, "", nil
}

// outputHasLicense reports whether the source directory already holds a
// license-like file (used to avoid a false "no attribution" warning).
func outputHasLicense(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		for _, cand := range licenseNames {
			if strings.EqualFold(e.Name(), cand) {
				return true
			}
		}
	}
	return false
}
