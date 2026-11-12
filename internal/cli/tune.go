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

package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/app"
	"harnessator.org/harnessator/internal/dialect"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/provider"
)

// tuneCmd is the guided checkup-and-repair pass:
//
//	does my harness config hold together - and is its model catalog TRUE?
//
// It walks phases (config, pins, auth, consistency, live probe, catalog
// reconcile, re-render), narrates every step, and ends with a summary.
// Failures come with directions: a missing env var gets the exact export
// line and (interactively) a one-shot offer to set it for this run only -
// harnessor never stores secrets. Catalog corrections land in a dev
// overlay (harnessator dev) so pinned sources stay pristine and the fix
// flows onward via contribute; --check-only reports, --yes automates.
func tuneCmd(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("tune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("config", "harnessator.toml", "project config file")
	checkOnly := fs.Bool("check-only", false, "report everything, change nothing")
	noProbe := fs.Bool("no-probe", false, "skip live gateway probes (offline checkup)")
	yes := fs.Bool("yes", false, "apply fixes without asking (non-interactive)")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: harnessator tune [--check-only] [--no-probe] [--yes] [--config <file>]")
		return 2
	}
	t := &tuner{
		out: stdout, errw: stderr, file: *projectFile,
		checkOnly: *checkOnly, noProbe: *noProbe, yes: *yes,
		sc: bufio.NewScanner(stdin),
	}
	if f, ok := stdin.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			t.tty = fi.Mode()&os.ModeCharDevice != 0
		}
	}
	return t.run(context.Background())
}

type tuner struct {
	out, errw                    io.Writer
	file                         string
	sc                           *bufio.Scanner
	tty, yes, checkOnly, noProbe bool

	a              *app.App
	summary        []string // "area: outcome" lines for the final block
	issues         int      // open problems counted
	pendingUpdate  bool
	catalogApplied bool
	live           map[string][]string // style -> reachable ids (probe phase)
}

func (t *tuner) step(name string, n, total int) {
	fmt.Fprintf(t.out, "\n[%d/%d] %s\n", n, total, name)
}
func (t *tuner) okf(format string, a ...any)   { fmt.Fprintf(t.out, "  ok     "+format+"\n", a...) }
func (t *tuner) notef(format string, a ...any) { fmt.Fprintf(t.out, "  note   "+format+"\n", a...) }
func (t *tuner) badf(format string, a ...any) {
	fmt.Fprintf(t.out, "  PROBLEM "+format+"\n", a...)
	t.issues++
}
func (t *tuner) rec(area string, v any) {
	t.summary = append(t.summary, fmt.Sprintf("  %-11s %v", area, v))
}

func (t *tuner) confirm(q string) bool {
	if t.yes {
		return true
	}
	if !t.tty {
		fmt.Fprintf(t.out, "  %s - skipped (no terminal; pass --yes to apply)\n", q)
		return false
	}
	fmt.Fprintf(t.out, "  %s [y/N] ", q)
	if !t.sc.Scan() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(t.sc.Text())) {
	case "y", "yes":
		return true
	}
	return false
}

func (t *tuner) readSecret(prompt string) string {
	if !t.tty || t.yes || t.checkOnly {
		return ""
	}
	fmt.Fprintf(t.out, "  %s ", prompt)
	if !t.sc.Scan() {
		return ""
	}
	return strings.TrimSpace(t.sc.Text())
}

// run executes the phases; n/total is computed up front so the numbering
// never lies even when phases self-skip (their bodies just say so).
func (t *tuner) run(ctx context.Context) int {
	fmt.Fprintf(t.out, "harnessator tune - checkup and guided repair%s\n",
		map[bool]string{true: " (check-only: nothing will change)"}[t.checkOnly])
	a, err := app.Open(ctx, t.file, app.ModeAuto)
	if err != nil {
		fmt.Fprintf(t.errw, "tune: config/state unreadable: %v\n"+
			"  fix the message above first - every later phase depends on it\n", err)
		return 1
	}
	t.a = a

	phases := []func(context.Context){t.phaseConfig, t.phasePins, t.phaseAuth,
		t.phaseConsistency, t.phaseProbe, t.phaseReconcile, t.phaseApply}
	for i, fn := range phases {
		t.step(phaseNames[i], i+1, len(phases))
		fn(ctx)
	}

	problems, warnings, fail := runCheck(t.errw, t.file)
	fmt.Fprintf(t.out, "\n[final] checkup gate (the same checks harnessator check runs)\n")
	for _, p := range problems {
		t.badf("%s", p)
	}
	for _, w := range warnings {
		t.notef("%s", w)
	}
	if len(problems) == 0 && fail == 0 {
		t.okf("check: policy, lock digests, installed state all green")
	}

	fmt.Fprintln(t.out, "\ntune summary")
	fmt.Fprintln(t.out, strings.Join(t.summary, "\n"))
	if t.issues == 0 {
		fmt.Fprintln(t.out, "result:    all green")
		return 0
	}
	fmt.Fprintf(t.out, "result:    %d issue(s) need attention (see PROBLEM lines and notes above)\n", t.issues)
	return 1
}

var phaseNames = []string{"config", "pins", "auth", "consistency", "gateway probe", "model catalog", "re-render"}

func (t *tuner) phaseConfig(_ context.Context) {
	a := t.a
	t.okf("project %s", a.Project)
	t.notef("targets: %s", strings.Join(a.Cfg.Targets, ", "))
	for _, s := range a.Cfg.Sources {
		p, ok := a.Pins[s.Name]
		if !ok {
			t.badf("source %s has no pin (run setup)", s.Name)
			continue
		}
		t.notef("source %s @ %s (%s)", s.Name, p.Resolved, shortSHA(p.SHA))
	}
	if a.GovProvider == nil {
		t.notef("no provider profile in effect - gateway auth/probe/catalog phases are no-ops;")
		t.notef("  select one with: harnessator provider use <source>:<provider-id>")
		t.rec("provider", "none selected (skipped)")
		return
	}
	t.okf("provider %s (catalog: %d aliases, styles: %s)", a.GovProvider.Name,
		len(a.GovProvider.Models), strings.Join(sortedStyleKeys(a.GovProvider), ", "))
	t.rec("provider", a.GovRef.Source+":provider/"+a.GovRef.ID)
}

func (t *tuner) phasePins(ctx context.Context) {
	if t.a.GovProvider == nil && len(t.a.Cfg.Sources) == 0 {
		t.notef("no sources yet")
		return
	}
	var stale []string
	offline := false
	for _, s := range t.a.Cfg.Sources {
		pin, ok := t.a.Pins[s.Name]
		if !ok {
			continue
		}
		fresh, err := gitops.Sync(ctx, t.a.Store, s, true)
		if err != nil {
			t.notef("cannot check %s for updates (%v)", s.Name, err)
			offline = true
			continue
		}
		if fresh.SHA != pin.SHA {
			stale = append(stale, fmt.Sprintf("%s: %s -> %s", s.Name, shortSHA(pin.SHA), fresh.Resolved))
		}
	}
	switch {
	case offline:
		t.rec("pins", "offline (not checked)")
	case len(stale) == 0:
		t.okf("pins are current for all %d source(s)", len(t.a.Cfg.Sources))
		t.rec("pins", "current")
	case t.checkOnly:
		for _, s := range stale {
			t.notef("update available: %s (check-only: not applied)", s)
		}
		t.rec("pins", "updates available (not applied)")
	default:
		for _, s := range stale {
			t.notef("update available: %s", s)
		}
		if t.confirm("refresh pins and re-apply (a full update pass)?") {
			t.pendingUpdate = true
			t.rec("pins", "update accepted")
		} else {
			t.rec("pins", "update declined")
		}
	}
}

// authNames: every env var NAME the provider references (profile fallback
// + style overrides), deduped and sorted.
func authNames(p *provider.Profile) []string {
	seen := map[string]bool{}
	if p.AuthEnv != "" {
		seen[p.AuthEnv] = true
	}
	for _, s := range sortedStyleKeys(p) {
		if ep, ok := p.Style(s); ok && ep.AuthEnv != "" {
			seen[ep.AuthEnv] = true
		}
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (t *tuner) phaseAuth(_ context.Context) {
	p := t.a.GovProvider
	if p == nil {
		t.notef("skipped (no provider)")
		return
	}
	var missing []string
	for _, name := range authNames(p) {
		if os.Getenv(name) != "" {
			t.okf("%s is set", name)
			continue
		}
		missing = append(missing, name)
		t.badf("%s is NOT set - styles using it cannot authenticate", name)
		t.notef("  fix: export %s=\"<value>\" in your shell / your env.sh - never in git", name)
		t.notef("  harnessator renders NAMES into every config and never stores values")
		// offer a session-only value so the rest of THIS run can proceed
		if v := t.readSecret(fmt.Sprintf("or paste a value for %s now (this run only, never stored):", name)); v != "" {
			if err := os.Setenv(name, v); err != nil {
				t.badf("cannot set %s in this process: %v", name, err)
			} else {
				t.okf("%s set for this tune run only - persist it yourself", name)
				missing = removeStr(missing, name)
				t.issues--
			}
		}
	}
	switch {
	case len(missing) == 0:
		t.rec("auth", "all referenced env vars present")
	case t.checkOnly:
		t.rec("auth", fmt.Sprintf("%d var(s) missing (check-only)", len(missing)))
	default:
		t.rec("auth", fmt.Sprintf("MISSING %s", strings.Join(missing, ", ")))
	}
}

func removeStr(xs []string, x string) []string {
	var out []string
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func (t *tuner) phaseConsistency(_ context.Context) {
	a := t.a
	for _, w := range a.GovWarnings {
		t.badf("%s", w)
	}
	if a.GovPolicy != nil && a.GovProvider != nil {
		for _, p := range a.GovPolicy.Validate(a.GovProvider) {
			t.badf("policy: %s", p)
		}
	}
	p := a.GovProvider
	for _, target := range a.Cfg.Targets {
		d, ok := dialect.For(target)
		if !ok {
			continue // no dialect = target does not take governance; setup reports it
		}
		if p == nil {
			continue
		}
		if _, _, ok := p.StylesSpoken(d.Styles()); !ok {
			t.badf("target %q speaks none of provider %q's styles (%v) - governance is skipped for it loudly at setup",
				target, p.Name, sortedStyleKeys(p))
		}
	}
	if a.GovProvider != nil && a.GovProvider.DefaultModel == "" {
		t.notef("no default_model in provider %s - harnesses fall back to their own default", a.GovProvider.Name)
	}
	if t.issues == 0 {
		t.okf("config, policy and dialects hold together")
		t.rec("consistency", "clean")
	} else {
		t.rec("consistency", "see PROBLEM lines")
	}
}

func (t *tuner) phaseProbe(ctx context.Context) {
	p := t.a.GovProvider
	if p == nil {
		t.notef("skipped (no provider)")
		return
	}
	if t.noProbe {
		t.notef("--no-probe: gateway reachability and catalog truth not checked")
		t.rec("probe", "skipped (--no-probe)")
		return
	}
	t.live = map[string][]string{}
	var parts []string
	for _, style := range sortedStyleKeys(p) {
		ep, _ := p.Style(style)
		key := ""
		if ep.AuthEnv != "" {
			key = os.Getenv(ep.AuthEnv)
		}
		if key == "" {
			t.badf("%s: no auth value in %s - cannot probe", style, ep.AuthEnv)
			parts = append(parts, style+": no auth")
			continue
		}
		ids, err := provider.Probe(ctx, style, ep, key)
		if err != nil {
			t.badf("%s: %v", style, err)
			parts = append(parts, style+": unreachable")
			continue
		}
		t.live[style] = ids
		t.okf("%s: %d models reachable at %s", style, len(ids), ep.BaseURL)
		parts = append(parts, fmt.Sprintf("%s: %d", style, len(ids)))
	}
	t.rec("probe", strings.Join(parts, " | "))
}

func (t *tuner) phaseReconcile(_ context.Context) {
	p := t.a.GovProvider
	if p == nil || len(t.live) == 0 {
		if p != nil {
			t.notef("no successful probe - catalog truth unknown this run")
		}
		return
	}
	union := map[string]bool{}
	for _, ids := range t.live {
		for _, id := range ids {
			union[id] = true
		}
	}
	adds, drops := reconcileCatalog(p, union)
	if len(adds) == 0 && len(drops) == 0 {
		t.okf("catalog matches the gateway exactly (%d aliases, %d reachable ids)",
			len(p.Models), len(union))
		t.rec("catalog", "in sync")
		return
	}
	for _, d := range drops {
		t.notef("stale alias %s -> %s is no longer reachable", d.Alias, d.ID)
	}
	for _, a := range adds {
		t.notef("new model %s reachable (suggested alias %q)", a.ID, a.Alias)
	}
	if t.checkOnly {
		t.rec("catalog", fmt.Sprintf("%d stale, %d new (check-only)", len(drops), len(adds)))
		return
	}
	srcName := t.a.GovRef.Source
	if !t.confirm(fmt.Sprintf("reconcile the catalog in %s (drop stale, add chosen new)?", srcName+":provider/"+t.a.GovRef.ID)) {
		t.rec("catalog", fmt.Sprintf("%d stale, %d new - declined", len(drops), len(adds)))
		return
	}
	if chosen, ok := t.pickAdds(adds); ok {
		adds = chosen
	} else {
		t.rec("catalog", "cancelled at add-selection")
		return
	}
	if err := t.applyCatalog(adds, drops); err != nil {
		t.badf("catalog not updated: %v", err)
		t.rec("catalog", "apply failed (see above)")
		return
	}
	t.catalogApplied = true
	strictNew := ""
	if t.a.GovPolicy != nil && t.a.GovPolicy.Strict && len(adds) > 0 {
		strictNew = " - new aliases need the policy's allowed list (a source too); until then setup skips them loudly"
	}
	t.okf("catalog reconciled in the dev overlay%s", strictNew)
	t.rec("catalog", fmt.Sprintf("-%d stale +%d new via dev overlay (contribute to upstream)", len(drops), len(adds)))
}

// pickAdds lets a human choose which new models to add; --yes takes all.
func (t *tuner) pickAdds(adds []catalogAdd) ([]catalogAdd, bool) {
	if t.yes || !t.tty {
		return adds, true
	}
	for i, a := range adds {
		fmt.Fprintf(t.out, "  %2d) %s  (alias %s)\n", i+1, a.ID, a.Alias)
	}
	fmt.Fprintf(t.out, "  add which? [a]=all, [n]=none or numbers like 1,3: ")
	if !t.sc.Scan() {
		return nil, false
	}
	ans := strings.ToLower(strings.TrimSpace(t.sc.Text()))
	switch ans {
	case "a", "all", "":
		return adds, true
	case "n", "none":
		return nil, false
	}
	var out []catalogAdd
	for _, part := range strings.Split(ans, ",") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &n); err == nil && n >= 1 && n <= len(adds) {
			out = append(out, adds[n-1])
		}
	}
	return out, len(out) > 0
}

// applyCatalog ensures a dev overlay for the provider's source (the ONLY
// sanctioned writable copy of source content), then rewrites the [models]
// section as text - comments and layout survive, a Parse failure restores
// the original byte-for-byte.
func (t *tuner) applyCatalog(adds []catalogAdd, drops []catalogDrop) error {
	srcName := t.a.GovRef.Source
	if _, active := t.a.Dev[srcName]; !active {
		if !t.confirm(fmt.Sprintf("create a dev overlay for %s to hold this change (harnessator dev; contribute pushes it up)?", srcName)) {
			return fmt.Errorf("no dev overlay - refusing to edit the pinned checkout")
		}
		if rc := devCmd(t.out, t.errw, []string{"--config", t.file, srcName}); rc != 0 {
			return fmt.Errorf("starting the dev overlay failed (exit %d) - do it manually, then tune again", rc)
		}
		a, err := app.Open(context.Background(), t.file, app.ModeAuto)
		if err != nil {
			return err
		}
		t.a = a // GovFile now points into the overlay
	}
	if t.a.GovFile == "" {
		return fmt.Errorf("provider component file unknown after reload")
	}
	if dev := t.a.Store.DevDir(srcName); !strings.HasPrefix(t.a.GovFile, dev+string(filepath.Separator)) {
		// Invariant: tune writes ONLY inside a dev overlay - the pinned
		// store checkout is immutable by governance design.
		return fmt.Errorf("refusing to edit %s: not inside the dev overlay %s", t.a.GovFile, dev)
	}
	orig, err := os.ReadFile(t.a.GovFile)
	if err != nil {
		return err
	}
	updated := applyCatalogEdits(string(orig), adds, drops)
	tmp := t.a.GovFile + ".tune.tmp"
	if err := os.WriteFile(tmp, []byte(updated), 0o644); err != nil {
		return err
	}
	if _, err := provider.Parse(tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("resulting profile failed validation (%v) - original kept", err)
	}
	if err := os.Rename(tmp, t.a.GovFile); err != nil {
		return err
	}
	return nil
}

func (t *tuner) phaseApply(_ context.Context) {
	if t.checkOnly {
		if t.pendingUpdate || t.catalogApplied {
			t.rec("re-render", "would run setup (check-only)")
		}
		return
	}
	if !t.pendingUpdate && !t.catalogApplied {
		t.notef("nothing to re-apply (no changes made)")
		t.rec("re-render", "not needed")
		return
	}
	mode := app.ModeAuto
	if t.pendingUpdate {
		mode = app.ModeFetch // a full update pass
	}
	a, err := app.Open(context.Background(), t.file, mode)
	if err != nil {
		t.badf("re-open failed: %v", err)
		return
	}
	t.a = a
	if err := a.Setup(context.Background(), false, indentWriter(t.out)); err != nil {
		t.badf("setup failed: %v", err)
		return
	}
	t.okf("setup re-applied (%s)", map[bool]string{true: "with refreshed pins"}[t.pendingUpdate])
	t.rec("re-render", "setup ok")
	if t.a.GovProvider == nil && t.catalogApplied {
		t.notef("reload lost the provider? run: harnessator provider use %s", t.a.GovRef.QualifiedID())
	}
	fmt.Fprintln(t.out, "  reminder: restart your harness to pick up the new configuration")
}

// --- pure catalog reconciliation (unit-tested) -----------------------------

type catalogAdd struct{ ID, Alias string }
type catalogDrop struct{ Alias, ID string }

// reconcileCatalog diffs the provider's catalog against the reachable set:
// aliases whose real id vanished drop, reachable ids nobody references add.
func reconcileCatalog(p *provider.Profile, live map[string]bool) ([]catalogAdd, []catalogDrop) {
	var drops []catalogDrop
	taken := map[string]bool{}
	for _, alias := range p.SortedAliasList() {
		taken[alias] = true
		id := p.Models[alias]
		if !live[id] {
			drops = append(drops, catalogDrop{Alias: alias, ID: id})
		}
	}
	var adds []catalogAdd
	for _, id := range sortedKeysBool(live) { // sorted: alias suggestions are deterministic
		if p.HasModelID(id) {
			continue
		}
		alias := suggestAlias(id, taken)
		adds = append(adds, catalogAdd{ID: id, Alias: alias})
		taken[alias] = true
	}
	return adds, drops
}

// suggestAlias derives a clean alias from a raw model id: lowercase,
// id-safe characters, capped, never colliding with taken.
func suggestAlias(id string, taken map[string]bool) string {
	s := strings.ToLower(id)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if i := strings.Index(out, "-"); i > 0 && len(out) > 40 {
		out = out[i+1:] // drop a vendor prefix when long: "anthropic-claude-x" -> "claude-x"
	}
	if out == "" || !(out[0] >= 'a' && out[0] <= 'z' || out[0] >= '0' && out[0] <= '9') {
		out = "m-" + out
	}
	for taken[out] {
		out = bumpSuffix(out)
	}
	if len(out) > 48 {
		out = out[len(out)-48:]
		// only [a-z0-9.-] can reach here, so trimming a bad first
		// character is safe (no padding games that break the cap)
		out = strings.TrimLeft(out, "-.")
		if out == "" || out[0] == '+' {
			out = "m" + strings.TrimLeft(out[1:], "-.")
		}
	}
	return out
}

func bumpSuffix(s string) string {
	i := len(s) - 1
	for i >= 0 && s[i] >= '0' && s[i] <= '9' {
		i--
	}
	if i >= 0 && s[i] == '-' && i < len(s)-1 {
		n := 0
		fmt.Sscanf(s[i+1:], "%d", &n)
		return s[:i+1] + fmt.Sprint(n+1)
	}
	return s + "-2"
}

// applyCatalogEdits rewrites the [models] section as text: dropped alias
// lines go, added pairs go in right after the header, a missing section
// is appended at the end. Everything outside [models] is untouched.
func applyCatalogEdits(src string, adds []catalogAdd, drops []catalogDrop) string {
	lines := strings.Split(src, "\n")
	var out []string
	inModels, haveModels := false, false
	dropSet := map[string]bool{}
	for _, d := range drops {
		dropSet[d.Alias] = true
	}
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		switch {
		case t == "[models]":
			haveModels = true
			inModels = true
			out = append(out, ln)
			continue
		case strings.HasPrefix(t, "["):
			inModels = false
		case inModels:
			if k, _, ok := strings.Cut(t, "="); ok && dropSet[strings.TrimSpace(k)] {
				continue // dropped
			}
		}
		out = append(out, ln)
	}
	var addLines []string
	for _, a := range adds {
		addLines = append(addLines, fmt.Sprintf("%s = %q", a.Alias, a.ID))
	}
	if len(addLines) == 0 {
		return strings.Join(out, "\n")
	}
	if !haveModels {
		out = append(out, "", "[models]")
		out = append(out, addLines...)
		return strings.Join(out, "\n")
	}
	// insert after the [models] header line
	for i, ln := range out {
		if strings.TrimSpace(ln) == "[models]" {
			rest := out[i+1:]
			out = append(out[:i+1:i+1], append(addLines, rest...)...)
			break
		}
	}
	return strings.Join(out, "\n")
}

func sortedKeysBool(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
