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

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteKickstartBrief(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "AGENTS.md"), "# rules\n\n## test first\nx\n\n## commit scoped\ny\n")

	file, wrote, err := WriteKickstart([]string{base}, false)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if filepath.Base(file) != KickstartFileName {
		t.Errorf("file = %q", file)
	}
	raw, _ := os.ReadFile(file)
	got := string(raw)
	for _, want := range []string{"HARNESSATOR KICKSTART", "AGENTS.md", "2 rules", "The mission", "harnessator harvest", "kickstart-test... "} {
		if want == "kickstart-test... " {
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("brief missing %q", want)
		}
	}
	// idempotent: refuses to touch an existing brief without force
	if _, wrote, err := WriteKickstart([]string{base}, false); err != nil || wrote {
		t.Errorf("second write: wrote=%v err=%v (want refusal without force)", wrote, err)
	}
	if _, wrote, err := WriteKickstart([]string{base}, true); err != nil || !wrote {
		t.Errorf("forced write: wrote=%v err=%v", wrote, err)
	}
}
