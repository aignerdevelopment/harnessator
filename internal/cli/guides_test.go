package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGuideCommands is the guide half of the docs contract: every
// `harnessator ...` invocation inside a code fence in docs/guides must
// use a verb the CLI actually dispatches and only flags it actually
// declares. Content rot beyond names is caught by scripts/run-guides.sh
// (executes every ```sh block on a clean sandbox) before releases.
func TestGuideCommands(t *testing.T) {
	verbs := map[string]bool{}
	for _, v := range commandVerbs(t) {
		verbs[v] = true
	}
	flags := map[string]bool{}
	for _, f := range declaredFlags(t) {
		flags[f] = true
	}

	re := regexp.MustCompile(`^\s*(?:\$ )?harnessator (\S+)(.*)$`)
	root := filepath.Join("..", "..", "docs", "guides")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		inFence := false
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if !inFence {
				continue
			}
			m := re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			verb, rest := m[1], m[2]
			if !verbs[verb] {
				t.Errorf("%s:%d: unknown verb %q", p, i+1, verb)
			}
			for _, tok := range strings.Fields(rest) {
				if !strings.HasPrefix(tok, "-") {
					continue
				}
				name := strings.TrimLeft(tok, "-")
				if j := strings.Index(name, "="); j >= 0 {
					name = name[:j]
				}
				if name != "" && !flags[name] {
					t.Errorf("%s:%d: unknown flag --%s", p, i+1, name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
