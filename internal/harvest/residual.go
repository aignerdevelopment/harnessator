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
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ResidualReport is the machine-readable handoff of the guided tail:
// what harvest deliberately left behind for the
// harnessator-harvest skill / a human to finish. Same versioned-envelope
// discipline as scan reports, so dashboards can render both.
type ResidualReport struct {
	Schema int             `json:"schema"`
	Output string          `json:"output"`
	Donors []DonorResidual `json:"donors"`
	Hint   string          `json:"hint"`
}

// DonorResidual groups leftovers by donor root.
type DonorResidual struct {
	Root     string     `json:"root"`
	Residual []Residual `json:"residual,omitempty"`
	Linked   []Linked   `json:"linked,omitempty"` // external/managed links: context, not work
}

// writeResidualReport emits harvest-report.json in the output source when
// anything was left behind; returns the residual count. The report is
// informational - it never blocks a harvest.
func writeResidualReport(outDir string, roots []Root) (int, error) {
	rep := ResidualReport{Schema: 1, Output: outDir}
	n := 0
	for _, r := range roots {
		if len(r.Residual) == 0 && len(r.Linked) == 0 {
			continue
		}
		sort.Slice(r.Residual, func(i, j int) bool { return r.Residual[i].Path < r.Residual[j].Path })
		rep.Donors = append(rep.Donors, DonorResidual{
			Root: r.Path, Residual: r.Residual, Linked: r.Linked,
		})
		n += len(r.Residual)
	}
	if n == 0 {
		return 0, nil
	}
	rep.Hint = "entries were left behind; for guided completion run the " +
		"harnessator-harvest skill in your harness, then ratify with " +
		"'harnessator harvest --commit " + outDir + "'"
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return n, err
	}
	return n, os.WriteFile(filepath.Join(outDir, "harvest-report.json"), append(b, '\n'), 0o644)
}
