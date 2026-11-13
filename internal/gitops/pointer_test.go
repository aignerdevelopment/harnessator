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

package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"harnessator.org/harnessator/internal/config"
	"harnessator.org/harnessator/internal/store"
)

func TestSyncContentResolvesAndExports(t *testing.T) {
	repo := mkRepo(t)
	st := store.Store{Root: t.TempDir()}
	ctx := context.Background()

	resolved, sha, dir, err := SyncContent(ctx, st, "sp", "", config.Content{Git: "file://" + repo, Ref: "^v1.0"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "v1.1.0" || len(sha) != 40 {
		t.Errorf("resolved %q sha %q", resolved, sha)
	}
	if b, rerr := os.ReadFile(filepath.Join(dir, ".harnessator-checkout")); rerr != nil || strings.TrimSpace(string(b)) != sha {
		t.Errorf("content checkout marker: %v %q", rerr, b)
	}
	// the content tree IS what landed: range pins v1.1.0, not the head
	if b, err := os.ReadFile(filepath.Join(dir, "content.txt")); err != nil || string(b) != "v2\n" {
		t.Errorf("exported content: %v %q", err, b)
	}
	// empty ref defaults to main (upstream branch names are not ours to assume)
	resolved, _, _, err = SyncContent(ctx, st, "sp", "", config.Content{Git: "file://" + repo}, true)
	if err != nil || resolved != "main" {
		t.Errorf("default ref: %q %v", resolved, err)
	}
}

func TestSyncContentIdentityGuard(t *testing.T) {
	a, b := mkRepo(t), mkRepo(t)
	st := store.Store{Root: t.TempDir()}
	ctx := context.Background()
	if _, _, _, err := SyncContent(ctx, st, "sp", "", config.Content{Git: "file://" + a, Ref: "main"}, true); err != nil {
		t.Fatal(err)
	}
	// the content clone namespace borrows the source's name - so a
	// re-point to ANOTHER repo under the same name refuses exactly like
	// the manifest side does.
	_, _, _, err := SyncContent(ctx, st, "sp", "", config.Content{Git: "file://" + b, Ref: "main"}, true)
	if err == nil || !strings.Contains(err.Error(), "one store name, two repositories") {
		t.Fatalf("want content aliasing refusal, got %v", err)
	}
}
