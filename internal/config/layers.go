package config

// Scope layers: up to five files, all sharing the project schema, are
// merged into one effective config. A file's LOCATION is its layer - no
// per-file marker needed. All layers except the project file are
// optional; parse errors in any present file fail loudly.
//
//	corporate  /etc/harnessator/config.toml        (Windows: %ProgramData%)
//	team       <project>/harnessator.team.toml
//	project    <project>/harnessator.toml
//	personal   <store>/personal.toml               (machine-wide)
//	local      <project>/.harnessator/local.toml   (per-project personal)
//
// Precedence (lowest first) is schema.Layers. Merge is additive:
// sources union by name (same name + different git = conflict, never a
// silent pick), targets/templates/selects concatenate in layer order,
// and scalar selections (provider) take the highest layer that sets
// them - so a developer's personal provider override beats the team
// default, while team structure can never be silently deleted by a
// higher layer (steer via excludes, visible in provenance).
//
// Every merged entry records the layer that introduced it (prov), so
// resolution can annotate "why" provenance with the physical file.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"harnessator.org/harnessator/internal/schema"
)

// SystemConfigFile is the corporate layer's conventional location.
func SystemConfigFile() string {
	if runtime.GOOS == "windows" {
		dir := os.Getenv("ProgramData")
		if dir == "" {
			dir = `C:\ProgramData`
		}
		return filepath.Join(dir, "harnessator", "config.toml")
	}
	return "/etc/harnessator/config.toml"
}

// Layers is the discovered set of scope-layer files for one project.
type Layers struct {
	ProjectFile string
	StoreRoot   string
	Paths       map[string]string         // layer -> conventional path
	Cfgs        map[string]*ProjectConfig // layer -> parsed file (existing only)
	Loaded      []string                  // layers present, precedence order
}

// LoadLayers reads every existing layer file. The project file itself
// must exist (it is the anchor); all other layers are optional.
func LoadLayers(storeRoot, projectFile string) (*Layers, error) {
	dir := filepath.Dir(projectFile)
	l := &Layers{
		ProjectFile: projectFile,
		StoreRoot:   storeRoot,
		Paths: map[string]string{
			"corporate": SystemConfigFile(),
			"team":      filepath.Join(dir, "harnessator.team.toml"),
			"project":   projectFile,
			"personal":  filepath.Join(storeRoot, "personal.toml"),
			"local":     filepath.Join(dir, ".harnessator", "local.toml"),
		},
		Cfgs: map[string]*ProjectConfig{},
	}
	for _, layer := range schema.Layers {
		p := l.Paths[layer]
		if _, err := os.Stat(p); err != nil {
			continue
		}
		cfg, err := LoadProjectConfig(p)
		if err != nil {
			return nil, fmt.Errorf("%s layer: %w", layer, err)
		}
		l.Cfgs[layer] = cfg
		l.Loaded = append(l.Loaded, layer)
	}
	if _, ok := l.Cfgs["project"]; !ok {
		return nil, fmt.Errorf("%s: project config not found", projectFile)
	}
	// fail fast: layer conflicts (e.g. same source name, different URL)
	// are surfaced at load time, not at first resolution
	if _, err := l.Effective(); err != nil {
		return nil, err
	}
	return l, nil
}

// LayerCfg returns the parsed config for a layer, or a fresh empty one
// (kind/schema pre-set) for a layer whose file does not exist yet - the
// mutation path for `enable --layer personal` and friends.
func (l *Layers) LayerCfg(layer string) (*ProjectConfig, error) {
	if !schema.IsLayer(layer) {
		return nil, fmt.Errorf("unknown layer %q (corporate|team|project|personal|local)", layer)
	}
	if c, ok := l.Cfgs[layer]; ok {
		return c, nil
	}
	return &ProjectConfig{Kind: "project", Schema: schema.SchemaVersion, Sources: []Source{}}, nil
}

// WriteLayer persists one layer's config to its conventional path.
// Corporate lives outside the user's write scope by design; a plain
// permission error with a pointer to the path is the honest answer.
func (l *Layers) WriteLayer(layer string, c *ProjectConfig) error {
	p := l.Paths[layer]
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := WriteProject(p, c); err != nil {
		if layer == "corporate" {
			return fmt.Errorf("%s: %w (the corporate layer is IT-managed - edit %s with admin rights or distribute it via your config management)", p, err, p)
		}
		return err
	}
	l.Cfgs[layer] = c
	return nil
}

// Effective merges all present layers (ascending precedence) into the
// single config resolution plans against. Entries are annotated with
// the layer that introduced them for "why" provenance.
func (l *Layers) Effective() (*ProjectConfig, error) {
	eff := &ProjectConfig{Kind: "project", Schema: schema.SchemaVersion, Sources: []Source{}}
	prov := map[string]string{}
	seenSrc := map[string]int{} // source name -> index in eff.Sources
	seenTmpl := map[string]bool{}
	seenSel := map[string]bool{}
	seenTarget := map[string]bool{}

	for _, layer := range schema.Layers {
		c, ok := l.Cfgs[layer]
		if !ok {
			continue
		}
		for _, t := range c.Targets {
			if !seenTarget[t] {
				seenTarget[t] = true
				eff.Targets = append(eff.Targets, t)
			}
		}
		for _, t := range c.Templates {
			if !seenTmpl[t] {
				seenTmpl[t] = true
				eff.Templates = append(eff.Templates, t)
			}
			if _, in := prov["template\x00"+t]; !in {
				prov["template\x00"+t] = layer
			}
		}
		for _, s := range c.Sources {
			if i, dup := seenSrc[s.Name]; dup {
				if eff.Sources[i].Git != s.Git {
					return nil, fmt.Errorf("source %q is declared with different git URLs by layers %q (%s) and %q (%s) - rename one or align them",
						s.Name, layerAt(l, prov, eff.Sources[i].Name), l.sourceOf(eff, s.Name), layer, l.Paths[layer])
				}
				eff.Sources[i].Layer = layer // highest declaring layer wins the label
				continue
			}
			seenSrc[s.Name] = len(eff.Sources)
			declared := s
			declared.Layer = layer
			eff.Sources = append(eff.Sources, declared)
		}
		for cat, sel := range c.Select {
			for _, e := range sel.Include {
				if key := "i\x00" + cat + "\x00" + e; !seenSel[key] {
					seenSel[key] = true
					s := eff.EnsureSelect(cat)
					s.Include = append(s.Include, e)
					eff.PutSelect(cat, s)
				}
				if pk := "include\x00" + cat + "\x00" + e; prov[pk] == "" {
					prov[pk] = layer
				}
			}
			for _, e := range sel.Exclude {
				if key := "x\x00" + cat + "\x00" + e; !seenSel[key] {
					seenSel[key] = true
					s := eff.EnsureSelect(cat)
					s.Exclude = append(s.Exclude, e)
					eff.PutSelect(cat, s)
				}
				if pk := "exclude\x00" + cat + "\x00" + e; prov[pk] == "" {
					prov[pk] = layer
				}
			}
		}
		if c.Provider != nil {
			cp := *c.Provider
			eff.Provider = &cp // highest layer that names one wins
		}
		if c.Policy != nil {
			if eff.Policy == nil {
				eff.Policy = &PolicyConfig{}
			}
			eff.Policy.Profiles = append(eff.Policy.Profiles, c.Policy.Profiles...)
		}
		if c.Corporate != nil {
			if eff.Corporate == nil {
				eff.Corporate = &CorporateConfig{}
			}
			eff.Corporate.Allowlist = union(eff.Corporate.Allowlist, c.Corporate.Allowlist)
			if c.Corporate.LockMode == "strict" {
				eff.Corporate.LockMode = "strict"
			} else if eff.Corporate.LockMode == "" {
				eff.Corporate.LockMode = c.Corporate.LockMode
			}
		}
		if c.Personal != "" {
			eff.Personal = c.Personal
		}
	}
	eff.setProvenance(prov)
	return eff, nil
}

func (l *Layers) sourceOf(eff *ProjectConfig, name string) string {
	for _, s := range eff.Sources {
		if s.Name == name {
			if s.Layer != "" {
				return l.Paths[s.Layer]
			}
		}
	}
	return "?"
}

// layerAt is only used for the conflict message; the effective source
// label carries the declaring layer.
func layerAt(_ *Layers, _ map[string]string, name string) string { return name }

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
