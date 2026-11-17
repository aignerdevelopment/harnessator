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
	"flag"
	"io"
	"testing"
)

// The documented CLI interface is mixed-order friendly
// (`enable <ref> --config p` == `enable --config p <ref>`); parseFlags
// makes that true despite Go's flag package stopping at positionals.
func TestParseFlagsMixedOrder(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "n")
	all := fs.Bool("all", false, "a")
	if err := parseFlags(fs, []string{"pos1", "--name", "harbor", "--all", "pos2"}); err != nil {
		t.Fatal(err)
	}
	if *name != "harbor" || !*all {
		t.Errorf("flags not applied from mixed order: name=%q all=%v", *name, *all)
	}
	if fs.NArg() != 2 || fs.Arg(0) != "pos1" || fs.Arg(1) != "pos2" {
		t.Errorf("positionals lost: %v", fs.Args())
	}

	// "--" terminator: everything after is positional, even flag-lookalikes
	fs2 := flag.NewFlagSet("y", flag.ContinueOnError)
	fs2.SetOutput(io.Discard)
	n2 := fs2.String("n", "", "")
	if err := parseFlags(fs2, []string{"--", "-weird-path"}); err != nil {
		t.Fatal(err)
	}
	if fs2.NArg() != 1 || fs2.Arg(0) != "-weird-path" || *n2 != "" {
		t.Errorf("terminator broken: %v n=%q", fs2.Args(), *n2)
	}

	// bool flag must NOT swallow the following positional
	fs3 := flag.NewFlagSet("z", flag.ContinueOnError)
	fs3.SetOutput(io.Discard)
	b3 := fs3.Bool("json", false, "")
	if err := parseFlags(fs3, []string{"file", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !*b3 || fs3.NArg() != 1 || fs3.Arg(0) != "file" {
		t.Errorf("bool hoisting: json=%v args=%v", *b3, fs3.Args())
	}

	// --flag=value and unknown flags keep standard semantics
	fs4 := flag.NewFlagSet("w", flag.ContinueOnError)
	fs4.SetOutput(io.Discard)
	v4 := fs4.String("v", "", "")
	if err := parseFlags(fs4, []string{"p", "--v=inline"}); err != nil {
		t.Fatal(err)
	}
	if *v4 != "inline" || fs4.NArg() != 1 {
		t.Errorf("--flag=value: v=%q args=%v", *v4, fs4.Args())
	}
	fs5 := flag.NewFlagSet("u", flag.ContinueOnError)
	fs5.SetOutput(io.Discard)
	if err := parseFlags(fs5, []string{"p", "--ghost"}); err == nil {
		t.Error("unknown flag must still error")
	}
}
