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

// Package semverrange parses the ref syntax used in harnessator source
// entries: either a semver tag range that selects the best matching git tag
// (productive sources) or a plain branch name (dev sources).
//
// Supported range forms follow the familiar node-semver subset implemented
// with golang.org/x/mod/semver: "*", "any", "=1.2.3", ">=1.2", ">1.2.3",
// "<=2", "^1.2.3", "~1.2", and space/comma-joined conjunctions of those.
// Bare numeric versions ("1.2.3", "v1.2") are exact-match ranges; anything
// else is a branch name. Prerelease/build tags never match (a plain release
// policy for corporate pins); a bare version-looking ref that carries a
// prerelease suffix (e.g. the branch "v2-migration") is treated as a branch.
package semverrange

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// Kind distinguishes how a ref selects commits.
type Kind int

const (
	// KindRange matches semver tags.
	KindRange Kind = iota
	// KindBranch tracks a named branch (resolved by gitops later).
	KindBranch
	// KindAny matches every valid tag (latest wins).
	KindAny
)

// Range is a parsed ref: a tag-range expression or a branch name.
type Range struct {
	raw    string
	kind   Kind
	bounds []bound
}

// bound is one constraint: [lo, hi] with per-side inclusivity; an empty
// side is unbounded.
type bound struct {
	lo, hi string // normalized ("v"-prefixed)
	loIncl bool
	hiIncl bool
}

// Raw returns the original expression.
func (r Range) Raw() string { return r.raw }

// Kind reports whether r is a tag range, "any", or a branch name.
func (r Range) Kind() Kind { return r.kind }

// IsBranch reports whether r refers to a branch rather than tags.
func (r Range) IsBranch() bool { return r.kind == KindBranch }

// Parse turns a ref expression into a Range.
func Parse(raw string) (Range, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Range{}, errors.New("empty ref")
	}
	if s == "*" || s == "any" {
		return Range{raw: s, kind: KindAny}, nil
	}
	fields := strings.FieldsFunc(s, func(c rune) bool { return c == ',' || c == ' ' })
	if len(fields) == 0 {
		return Range{}, fmt.Errorf("ref %q: no content", raw)
	}
	if !rangeLike(fields) {
		if len(fields) == 1 {
			return Range{raw: s, kind: KindBranch}, nil
		}
		return Range{}, fmt.Errorf("ref %q: neither a valid range nor a single branch name", raw)
	}
	r := Range{raw: s, kind: KindRange}
	for _, f := range fields {
		b, err := parseBound(f)
		if err != nil {
			return Range{}, fmt.Errorf("ref %q: %w", raw, err)
		}
		r.bounds = append(r.bounds, b)
	}
	return r, nil
}

var bareVersion = regexp.MustCompile(`^v?\d+(\.\d+){0,2}$`)

// rangeLike reports whether the token list should be parsed as a tag range:
// at least one operator-prefixed token, or all bare release versions.
func rangeLike(fields []string) bool {
	for _, f := range fields {
		if hasOperator(f) {
			return true
		}
	}
	for _, f := range fields {
		if !bareVersion.MatchString(f) {
			return false
		}
	}
	return true
}

func hasOperator(s string) bool {
	for _, p := range []string{">=", "<=", "==", "~", "^", ">", "<", "="} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// parseBound handles one constraint token: operator + possibly partial
// version ("1.2" is 1.2.0), or a bare exact version.
func parseBound(tok string) (bound, error) {
	switch {
	case strings.HasPrefix(tok, ">="):
		return cmpBound(">=", tok[2:])
	case strings.HasPrefix(tok, "<="):
		return cmpBound("<=", tok[2:])
	case strings.HasPrefix(tok, "=="):
		return eqBound(tok[2:])
	case strings.HasPrefix(tok, "~"):
		return tildeBound(tok[1:])
	case strings.HasPrefix(tok, "^"):
		return caretBound(tok[1:])
	case strings.HasPrefix(tok, ">"):
		return cmpBound(">", tok[1:])
	case strings.HasPrefix(tok, "<"):
		return cmpBound("<", tok[1:])
	case strings.HasPrefix(tok, "="):
		return eqBound(tok[1:])
	default:
		return eqBound(tok)
	}
}

func cmpBound(op, vs string) (bound, error) {
	v, err := parseVersion(vs)
	if err != nil {
		return bound{}, err
	}
	b := bound{}
	switch op {
	case ">=":
		b.lo, b.loIncl = v, true
	case ">":
		b.lo = v
	case "<=":
		b.hi, b.hiIncl = v, true
	case "<":
		b.hi = v
	}
	return b, nil
}

func eqBound(vs string) (bound, error) {
	v, err := parseVersion(vs)
	if err != nil {
		return bound{}, err
	}
	return bound{lo: v, hi: v, loIncl: true, hiIncl: true}, nil
}

func tildeBound(vs string) (bound, error) {
	v, err := parseVersion(vs)
	if err != nil {
		return bound{}, err
	}
	b := bound{lo: v, loIncl: true}
	if components(vs) >= 2 {
		b.hi = bumpMinor(v) // ~1.2.3 -> [1.2.3, 1.3.0)
	} else {
		b.hi = bumpMajor(v) // ~1 behaves like ^1: [1.0.0, 2.0.0)
	}
	return b, nil
}

func caretBound(vs string) (bound, error) {
	v, err := parseVersion(vs)
	if err != nil {
		return bound{}, err
	}
	b := bound{lo: v, loIncl: true}
	switch {
	case semver.Major(v) == "v0" && minorOf(v) == 0 && components(vs) >= 3:
		b.hi = bumpPatch(v) // ^0.0.z pins the patch, like node-semver
	case semver.Major(v) == "v0":
		b.hi = bumpMinor(v) // ^0.y.z stays within the minor
	default:
		b.hi = bumpMajor(v)
	}
	return b, nil
}

// components counts the dotted numeric groups a user wrote ("1.2" -> 2).
func components(s string) int {
	return len(strings.Split(strings.TrimPrefix(s, "v"), "."))
}

func parseVersion(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("missing version")
	}
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		return "", fmt.Errorf("%q: prerelease/build refs are not supported", s)
	}
	nums := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(nums) > 3 {
		return "", fmt.Errorf("invalid version %q", s)
	}
	for _, n := range nums {
		if !isDigits(n) {
			return "", fmt.Errorf("invalid version %q", s)
		}
	}
	for len(nums) < 3 {
		nums = append(nums, "0")
	}
	v := "v" + strings.Join(nums, ".")
	if !semver.IsValid(v) {
		return "", fmt.Errorf("invalid version %q", s)
	}
	return v, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// x/mod/semver exposes only Major; Minor/Patch are trivial for our
// guaranteed-valid "vMAJOR.MINOR.PATCH" strings.

func minorOf(v string) int {
	n, _ := strconv.Atoi(strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)[1])
	return n
}

func patchOf(v string) int {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	n, _ := strconv.Atoi(parts[2])
	return n
}

func bumpMajor(v string) string {
	m, _ := strconv.Atoi(strings.TrimPrefix(semver.Major(v), "v"))
	return fmt.Sprintf("v%d.0.0", m+1)
}

func bumpMinor(v string) string {
	m, _ := strconv.Atoi(strings.TrimPrefix(semver.Major(v), "v"))
	return fmt.Sprintf("v%d.%d.0", m, minorOf(v)+1)
}

func bumpPatch(v string) string {
	m, _ := strconv.Atoi(strings.TrimPrefix(semver.Major(v), "v"))
	return fmt.Sprintf("v%d.%d.%d", m, minorOf(v), patchOf(v)+1)
}

// Match reports whether tag satisfies the range. Branch ranges never match
// tags; KindAny matches every valid, non-prerelease semver tag.
func (r Range) Match(tag string) bool {
	if r.kind == KindBranch {
		return false
	}
	v := normalizeTag(tag)
	if v == "" {
		return false
	}
	if r.kind == KindAny {
		return true
	}
	for _, b := range r.bounds {
		if b.lo != "" {
			if c := semver.Compare(v, b.lo); c < 0 || (c == 0 && !b.loIncl) {
				return false
			}
		}
		if b.hi != "" {
			if c := semver.Compare(v, b.hi); c > 0 || (c == 0 && !b.hiIncl) {
				return false
			}
		}
	}
	return true
}

// Latest returns the highest valid, non-prerelease tag satisfying r. For a
// branch Range there is no tag answer and ok is false.
func (r Range) Latest(tags []string) (string, bool) {
	best, bestV := "", ""
	for _, t := range tags {
		if !r.Match(t) {
			continue
		}
		v := normalizeTag(t)
		if bestV == "" || semver.Compare(v, bestV) > 0 {
			best, bestV = t, v
		}
	}
	return best, bestV != ""
}

// normalizeTag converts "1.2.3"/"v1.2.3" to canonical semver ("v1.2.3") or
// returns "" when the tag is not a plain release version.
func normalizeTag(tag string) string {
	t := strings.TrimSpace(tag)
	if t == "" {
		return ""
	}
	v := "v" + strings.TrimPrefix(t, "v")
	if !semver.IsValid(v) || semver.Prerelease(v) != "" {
		return ""
	}
	return v
}
