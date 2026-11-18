// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestPublicSurface is the contract freeze: the command list, every
// command's flag grammar (probed through the flag package's own usage
// dump), the help topics and the workflow names are captured in one
// golden file. Adding is allowed until 1.0; REMOVING or RENAMING
// anything fails here until someone regenerates the golden ON PURPOSE
// - which is exactly what a contract change looks like: deliberate,
// visible in the diff, announced in the CHANGELOG.
//
//	harnessator: the grammar probe is read-only by design - an unknown
//	flag must fail at PARSE time in every command; the assertion below
//	proves nothing was executed by feeding --zzz to all of them.
func TestPublicSurface(t *testing.T) {
	var b strings.Builder
	b.WriteString("# harnessator public surface - frozen grammar (see docs/wiki/Contract-v1.md)\n")
	b.WriteString("# change it deliberately:\n")
	b.WriteString("#   HARNESSATOR_UPDATE_SURFACE=1 go test ./internal/cli/ -run TestPublicSurface\n")

	b.WriteString("commands:\n")
	names := commandNames()
	if len(names) < 25 {
		t.Fatalf("dispatch parse found only %d commands - the parser broke, not the CLI", len(names))
	}
	usageFlag := regexp.MustCompile(`(?m)^  -([A-Za-z][A-Za-z0-9-]*)`)
	for _, name := range names {
		code, out, errb := runCLI(t, "", name, "--zzz-undefined-flag")
		combined := out + errb
		if strings.Contains(combined, "Usage of "+name+":") {
			if code != 2 {
				t.Errorf("%s: usage error must exit 2 (contract), got %d", name, code)
			}
			var flags []string
			for _, m := range usageFlag.FindAllStringSubmatch(combined, -1) {
				flags = appendUnique(flags, m[1])
			}
			sort.Strings(flags)
			b.WriteString("  " + name + ": --" + strings.Join(flags, " --") + "\n")
			continue
		}
		// no flag grammar (version/help-style: handled before any
		// FlagSet exists) - frozen as such, so a silent "we added a
		// hidden flag parser" also trips the golden
		b.WriteString("  " + name + ": (no flag grammar)\n")
	}

	b.WriteString("help-topics:\n")
	var topics []string
	for k := range commandHelp {
		topics = append(topics, k)
	}
	sort.Strings(topics)
	b.WriteString("  " + strings.Join(topics, " ") + "\n")

	b.WriteString("workflows:\n")
	var wf []string
	for _, w := range workflows {
		wf = append(wf, w.Name)
	}
	sort.Strings(wf)
	b.WriteString("  " + strings.Join(wf, " ") + "\n")

	golden := filepath.Join("testdata", "surface.golden")
	got := b.String()
	if os.Getenv("HARNESSATOR_UPDATE_SURFACE") != "" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("surface golden regenerated (%d bytes)", len(got))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("no surface golden yet - generate deliberately: HARNESSATOR_UPDATE_SURFACE=1 go test ./internal/cli/ -run TestPublicSurface (%v)", err)
	}
	if string(want) != got {
		t.Errorf("PUBLIC SURFACE CHANGED without a deliberate golden update.\n"+
			"diff (want -> got):\n%s\n"+
			"If this is an intended contract change: announce it in CHANGELOG.md, then regenerate.\n"+
			"If not: this test just prevented a breaking change - revert the offending edit.",
			shortDiff(string(want), got))
	}
}

func shortDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	var out []string
	for i := range al {
		if i >= len(bl) || al[i] != bl[i] {
			out = append(out, "- "+al[min(i, len(al)-1)])
			if i < len(bl) {
				out = append(out, "+ "+bl[i])
			}
			if len(out) > 20 {
				break
			}
		}
	}
	return strings.Join(out, "\n")
}
