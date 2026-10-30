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

// Package app is the stable core API every frontend binds to - the CLI,
// TUI, web and MCP all read the same internals: core is the product,
// frontends are views.
// It owns the pipeline config -> store -> plan -> materialize and the
// read-only state checks, without any flag/IO concerns.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/contenthash"
	"harnessator.org/harnessator/internal/gitops"
	"harnessator.org/harnessator/internal/lock"
	"harnessator.org/harnessator/internal/materialize"
	"harnessator.org/harnessator/internal/policy"
	"harnessator.org/harnessator/internal/provider"
	"harnessator.org/harnessator/internal/resolve"
	"harnessator.org/harnessator/internal/store"
)

// App is an opened project with fetched sources and a computed plan.
type App struct {
	Project    string // project root dir
	ConfigPath string
	Layers     *config.Layers // scope-layer files found; mutations route through them
	Cfg        *config.ProjectConfig
	Store      store.Store
	Pins       map[string]*gitops.Pinned
	Manifests  map[string]*config.SourceManifest
	Plan       *resolve.Plan
	Lock       *lock.Lock // nil when no lock yet
	FromLock   bool       // pins came from the lock (offline mode)

	// Governance: the selected provider catalog and the effective
	// model policy; strict violations fail Open itself.
	GovProvider *provider.Profile
	GovPolicy   *policy.Effective
	GovWarnings []string

	// Dev bindings (.harnessator/dev.json, gitignored): source name ->
	// branch. Pins for these sources point at the writable overlay.
	Dev map[string]string
}

// Open modes select how pins are determined.
type Mode int

const (
	// ModeFetch resolves refs over the network (setup/update, mutations).
	ModeFetch Mode = iota
	// ModeLocked uses harnessator.lock pins; fails if checkouts are absent.
	ModeLocked
	// ModeAuto prefers ModeLocked, falls back to ModeFetch.
	ModeAuto
)

func Open(ctx context.Context, configPath string, mode Mode) (*App, error) {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	st, err := store.Default()
	if err != nil {
		return nil, err
	}
	ls, err := config.LoadLayers(st.Root, abs)
	if err != nil {
		return nil, err
	}
	a := &App{Project: filepath.Dir(abs), ConfigPath: abs, Store: st, Layers: ls}
	if a.Cfg, err = ls.Effective(); err != nil {
		return nil, err
	}

	switch mode {
	case ModeLocked:
		if err := a.pinsFromLock(ctx); err != nil {
			return nil, err
		}
		a.FromLock = true
	case ModeAuto:
		if err := a.pinsFromLock(ctx); err == nil {
			a.FromLock = true
		} else if modeErr := a.pinsByFetch(ctx); modeErr != nil {
			return nil, fmt.Errorf("no usable lock (%v); fetch also failed: %w", err, modeErr)
		}
	default:
		if err := a.pinsByFetch(ctx); err != nil {
			return nil, err
		}
	}

	if a.Plan, err = resolve.Resolve(a.Cfg, a.Manifests); err != nil {
		return nil, err
	}
	if err := a.applyDevBindings(); err != nil {
		return nil, err
	}
	if err := a.loadGovernance(); err != nil {
		return nil, err
	}
	return a, nil
}

// devFile is the per-project dev binding store; generated, gitignored, and
// intentionally NOT part of the lock (dev state must never leak to
// teammates - isolation by design).
func (a *App) devFilePath() string {
	return filepath.Join(a.Project, ".harnessator", "dev.json")
}

// applyDevBindings rewires pinned sources onto their writable dev overlays.
func (a *App) applyDevBindings() error {
	data, err := os.ReadFile(a.devFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	bindings := map[string]struct {
		Branch string `json:"branch"`
	}{}
	if err := json.Unmarshal(data, &bindings); err != nil {
		return fmt.Errorf("%s: %w", a.devFilePath(), err)
	}
	a.Dev = map[string]string{}
	for name, b := range bindings {
		pin, ok := a.Pins[name]
		if !ok {
			a.GovWarnings = append(a.GovWarnings, "dev.json references unknown source "+name)
			continue
		}
		dir := a.Store.DevDir(name)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			a.GovWarnings = append(a.GovWarnings,
				fmt.Sprintf("dev overlay for %s missing at %s (dropped?) - running from pinned checkout", name, dir))
			continue
		}
		pin.Dir = dir
		pin.Resolved = b.Branch + " (dev)"
		a.Dev[name] = b.Branch
	}
	return nil
}

// WriteDevBindings persists the current dev state.
func (a *App) WriteDevBindings() error {
	if len(a.Dev) == 0 {
		return os.Remove(a.devFilePath())
	}
	m := map[string]map[string]string{}
	for name, branch := range a.Dev {
		m[name] = map[string]string{"branch": branch}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.devFilePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.devFilePath(), raw, 0o644)
}

// loadGovernance resolves the provider profile and model policy selections
// and enforces them: strict violations are an Open error (fail every
// command, not just setup), soft violations become GovWarnings.
func (a *App) loadGovernance() error {
	var prof *provider.Profile
	if a.Cfg.Provider != nil && a.Cfg.Provider.Profile != "" {
		ch, err := a.enabledChoice(a.Cfg.Provider.Profile)
		if err != nil {
			return fmt.Errorf("provider.profile: %w", err)
		}
		if ch.Category != "provider" {
			return fmt.Errorf("provider.profile %q references a %s component", a.Cfg.Provider.Profile, ch.Category)
		}
		p, err := provider.Parse(filepath.Join(a.Pins[ch.Source].ManifestDir(), filepath.FromSlash(ch.Component.Path)))
		if err != nil {
			return err
		}
		prof = p
	} else {
		var singles []resolve.Choice
		for _, ch := range a.Plan.Enabled {
			if ch.Category == "provider" {
				singles = append(singles, ch)
			}
		}
		switch len(singles) {
		case 1:
			p, err := provider.Parse(filepath.Join(a.Pins[singles[0].Source].ManifestDir(), filepath.FromSlash(singles[0].Component.Path)))
			if err != nil {
				return err
			}
			prof = p
		case 0:
		default:
			a.GovWarnings = append(a.GovWarnings,
				fmt.Sprintf("%d provider components enabled but none selected; set [provider].profile", len(singles)))
		}
	}

	var pols []*policy.Policy
	var ids []string
	collect := func(ref string) error {
		ch, err := a.enabledChoice(ref)
		if err != nil {
			return fmt.Errorf("policy.profiles: %w", err)
		}
		p, err := policy.Parse(filepath.Join(a.Pins[ch.Source].ManifestDir(), filepath.FromSlash(ch.Component.Path)))
		if err != nil {
			return err
		}
		pols = append(pols, p)
		ids = append(ids, ch.QualifiedID())
		return nil
	}
	if a.Cfg.Policy != nil && len(a.Cfg.Policy.Profiles) > 0 {
		for _, ref := range a.Cfg.Policy.Profiles {
			if err := collect(ref); err != nil {
				return err
			}
		}
	} else {
		for _, ch := range a.Plan.Enabled {
			if ch.Category == "policy" {
				p, err := policy.Parse(filepath.Join(a.Pins[ch.Source].ManifestDir(), filepath.FromSlash(ch.Component.Path)))
				if err != nil {
					return err
				}
				pols = append(pols, p)
				ids = append(ids, ch.QualifiedID())
			}
		}
	}

	a.GovProvider = prof
	if len(pols) == 0 {
		return nil
	}
	a.GovPolicy = policy.Merge(pols, ids)

	if prof == nil {
		if len(a.GovPolicy.Allowed) > 0 || a.GovPolicy.SubagentModel != "" {
			const msg = "model policy enabled but no provider profile selected - catalog cannot be enforced"
			if a.GovPolicy.Strict {
				return errors.New(msg)
			}
			a.GovWarnings = append(a.GovWarnings, msg)
		}
		return nil
	}
	if v := a.GovPolicy.Validate(prof); len(v) > 0 {
		if a.GovPolicy.Strict {
			return fmt.Errorf("model policy violations (strict):\n  %s", joinLines(v))
		}
		a.GovWarnings = append(a.GovWarnings, v...)
	}
	return nil
}

func (a *App) enabledChoice(ref string) (resolve.Choice, error) {
	src, cat, id := splitRefParts(ref)
	for _, ch := range a.Plan.Enabled {
		if ch.ID != id {
			continue
		}
		if src != "" && ch.Source != src {
			continue
		}
		if cat != "" && ch.Category != cat {
			continue
		}
		return ch, nil
	}
	return resolve.Choice{}, fmt.Errorf("%q is enabled by no template or select (see harnessator list --all)", ref)
}

func splitRefParts(ref string) (src, cat, id string) {
	rest := ref
	if s, r, ok := strings.Cut(rest, ":"); ok {
		src, rest = s, r
	}
	if c, i, ok := strings.Cut(rest, "/"); ok {
		cat, id = c, i
	} else {
		id = rest
	}
	return
}

func joinLines(v []string) string {
	return strings.Join(v, "\n  ")
}

func (a *App) pinsFromLock(ctx context.Context) error {
	l, err := lock.Read(filepath.Join(a.Project, "harnessator.lock"))
	if err != nil {
		return err
	}
	a.Lock = l
	for _, ls := range l.Sources {
		dir := a.Store.CheckoutDir(ls.Name, ls.SHA)
		if _, err := os.Stat(filepath.Join(dir, ".harnessator-checkout")); err != nil {
			return fmt.Errorf("source %s: locked checkout %s not in store; run setup/update", ls.Name, ls.SHA[:12])
		}
		pin := &gitops.Pinned{
			Name: ls.Name, Git: ls.Git, Range: ls.Range,
			Resolved: ls.Resolved, SHA: ls.SHA, Dir: dir,
			ManifestRoot: gitops.ManifestDirOf(dir, ls.Manifest),
		}
		if a.Pins == nil {
			a.Pins = map[string]*gitops.Pinned{}
			a.Manifests = map[string]*config.SourceManifest{}
		}
		m, err := config.LoadSourceManifest(filepath.Join(pin.ManifestDir(), "harnessator.toml"))
		if err != nil {
			return err
		}
		a.Pins[ls.Name] = pin
		a.Manifests[ls.Name] = m
	}
	// sources added to config since the lock must be fetched for a plan
	for _, s := range a.Cfg.Sources {
		if _, ok := a.Pins[s.Name]; !ok {
			return fmt.Errorf("source %s not in lock; run setup/update", s.Name)
		}
	}
	return nil
}

func (a *App) pinsByFetch(ctx context.Context) error {
	a.Pins = map[string]*gitops.Pinned{}
	a.Manifests = map[string]*config.SourceManifest{}
	for _, s := range a.Cfg.Sources {
		pin, err := gitops.Sync(ctx, a.Store, s, true)
		if err != nil {
			return err
		}
		m, err := config.LoadSourceManifest(filepath.Join(pin.ManifestDir(), "harnessator.toml"))
		if err != nil {
			return fmt.Errorf("source %s: %w", s.Name, err)
		}
		if err := config.ValidateSource(m, pin.ManifestDir()); err != nil {
			return fmt.Errorf("source %s: %w", s.Name, err)
		}
		a.Pins[s.Name] = pin
		a.Manifests[s.Name] = m
	}
	return nil
}

// MaterializeContext assembles the adapter context for this app state.
func (a *App) MaterializeContext(dry bool, log io.Writer) *materialize.Context {
	return &materialize.Context{
		Project:   a.Project,
		Manifests: a.Manifests,
		Pins:      a.Pins,
		Plan:      a.Plan,
		Provider:  a.GovProvider,
		Policy:    a.GovPolicy,
		DryRun:    dry,
		Log:       log,
	}
}

// Setup materializes everything and writes harnessator.lock. With dry,
// only the recorded report is produced.
func (a *App) Setup(ctx context.Context, dry bool, log io.Writer) error {
	mc := a.MaterializeContext(dry, log)
	if err := materialize.ApplyAll(mc); err != nil {
		return err
	}
	if err := materialize.WriteShared(mc); err != nil {
		return err
	}
	if dry {
		return nil
	}
	if len(a.Dev) > 0 {
		var names []string
		for n := range a.Dev {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(log, "  DEV MODE active for %v - lock NOT written (dev digests must never be committed)\n", names)
		return nil
	}
	l, err := materialize.BuildLock(mc)
	if err != nil {
		return err
	}
	a.Lock = l
	return l.Write(filepath.Join(a.Project, "harnessator.lock"))
}

// VerifyDrift returns problems when the store/digest state diverges from
// the lock (the CI gate). Empty slice means consistent.
func (a *App) VerifyDrift() ([]string, error) {
	if a.Lock == nil {
		l, err := lock.Read(filepath.Join(a.Project, "harnessator.lock"))
		if err != nil {
			return nil, err
		}
		a.Lock = l
	}
	var problems []string
	for _, ls := range a.Lock.Sources {
		dir := a.Store.CheckoutDir(ls.Name, ls.SHA)
		b, err := os.ReadFile(filepath.Join(dir, ".harnessator-checkout"))
		if err != nil || string(b) != ls.SHA+"\n" {
			problems = append(problems, fmt.Sprintf("source %s: pinned checkout %s missing or wrong", ls.Name, sh(ls.SHA)))
			continue
		}
	}
	for _, comp := range a.Lock.Components {
		if _, dev := a.Dev[comp.Source]; dev {
			continue // dev overlays are WIP by design; digest checks would lie
		}
		ls, ok := a.Lock.Source(comp.Source)
		if !ok {
			continue
		}
		dir := a.Store.CheckoutDir(ls.Name, ls.SHA)
		mdir := gitops.ManifestDirOf(dir, ls.Manifest)
		m, err := config.LoadSourceManifest(filepath.Join(mdir, "harnessator.toml"))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", comp.ID, err))
			continue
		}
		def, ok := lookupComponent(m, comp.ID)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: not in manifest anymore", comp.ID))
			continue
		}
		got, err := contenthash.TreeOrFile(filepath.Join(mdir, filepath.FromSlash(def.Path)))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", comp.ID, err))
			continue
		}
		if got != comp.Digest {
			problems = append(problems, fmt.Sprintf("%s: digest drifted (locked %s, now %s)", comp.ID, comp.Digest, got))
		}
	}
	return problems, nil
}

func lookupComponent(m *config.SourceManifest, qualifiedID string) (config.Component, bool) {
	_, rest, ok := strings.Cut(qualifiedID, ":")
	if !ok {
		return config.Component{}, false
	}
	cat, id, ok := strings.Cut(rest, "/")
	if !ok {
		return config.Component{}, false
	}
	c, ok := m.Components[cat][id]
	return c, ok
}

func sh(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// EnsureGitignore appends the generated-artifacts block when absent.
func EnsureGitignore(dir string) {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if err == nil && strings.Contains(string(data), ".harnessator/") {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(gitignoreBlock)
}

const gitignoreBlock = `
# >>> harnessator (generated artifacts; recreate with harnessator setup) >>>
.harnessator/
.mcp.json
.claude/
.opencode/
.codex/
.agents/
.cursor/
.pi/
# <<< harnessator <<<
`

const agentsTemplate = "# AGENTS.md\n\nThis project's AI-harness configuration is managed by harnessator.\nRead .harnessator/active.md and load only what it lists.\nChange harnessator.toml (not the generated artifacts), then run\n`harnessator setup` and restart the harness.\n"

const claudeTemplate = "# CLAUDE.md\n\nRead `AGENTS.md`, then follow .harnessator/active.md.\nConfiguration is managed by `harnessator setup`.\n"

// EnsureEntryFiles writes AGENTS.md/CLAUDE.md redirect files when absent.
func EnsureEntryFiles(dir string) {
	for name, tmpl := range map[string]string{"AGENTS.md": agentsTemplate, "CLAUDE.md": claudeTemplate} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		os.WriteFile(p, []byte(tmpl), 0o644)
	}
}

// DoctorNotes lists non-blocking advisories (vs. DoctorProblems which fail
// the run).
func (a *App) DoctorNotes() []string {
	var notes []string
	for _, s := range a.Cfg.Sources {
		if strings.HasPrefix(s.Git, "file://") {
			notes = append(notes,
				fmt.Sprintf("source %s points at a local path (%s) - fine for testing; publish it and set the real URL for collaborators", s.Name, s.Git))
		}
	}
	return notes
}

// DoctorProblems aggregates every state check: lock freshness, drift,
// materialization vs. declared specs, gitignore hygiene. Empty = healthy.
func (a *App) DoctorProblems() []string {
	var problems []string
	if !a.FromLock {
		problems = append(problems, "no lock in sync with fetched refs; run `harnessator update`")
	}
	drift, err := a.VerifyDrift()
	if err != nil {
		problems = append(problems, err.Error())
	} else if a.Lock == nil {
		problems = append(problems, "harnessator.lock missing; run `harnessator setup`")
	} else {
		problems = append(problems, drift...)
	}
	problems = append(problems, a.DoctorLinks()...)
	problems = append(problems, a.DoctorGitignore()...)
	return problems
}

// DoctorLinks compares the adapters' declared expectations against the
// project filesystem (missing/stray/wrong links and generated files).
func (a *App) DoctorLinks() []string {
	mc := a.MaterializeContext(true, io.Discard)
	// pins must have real manifests on disk for componentSrc; ModeAuto/Open
	// guarantees that already.
	return materialize.CheckSpecs(mc)
}

// DoctorGitignore verifies the generated dirs are ignored (the committed
// config+lock are the only harnessator files git should ever see there).
func (a *App) DoctorGitignore() []string {
	want := []string{".harnessator/", ".opencode/", ".claude/", ".mcp.json"}
	data, err := os.ReadFile(filepath.Join(a.Project, ".gitignore"))
	if err != nil {
		return []string{".gitignore unreadable: " + err.Error()}
	}
	var problems []string
	have := string(data)
	for _, w := range want {
		if !strings.Contains(have, w) {
			problems = append(problems, ".gitignore missing entry: "+w)
		}
	}
	return problems
}

// ErrNotConfigured is returned for projects without harnessator.toml.
var ErrNotConfigured = errors.New("no harnessator.toml in this directory; run harnessator init")

// LayerCfg returns the mutable config of one scope layer (a fresh empty
// project-shaped config when the file does not exist yet).
func (a *App) LayerCfg(layer string) (*config.ProjectConfig, error) {
	return a.Layers.LayerCfg(layer)
}

// WriteLayer persists one layer's file and refreshes the effective view.
func (a *App) WriteLayer(layer string, c *config.ProjectConfig) error {
	if err := a.Layers.WriteLayer(layer, c); err != nil {
		return err
	}
	eff, err := a.Layers.Effective()
	if err != nil {
		return err
	}
	a.Cfg = eff
	return nil
}
