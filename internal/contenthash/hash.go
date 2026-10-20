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

// Package contenthash computes stable digests over files and directory
// trees. Lock entries store one of these so a changed skill, rule or tool
// (even behind the same commit) is detectable without re-hashing the world.
//
// The construction sorts by slash-normalized relative path and hashes
// "path\x00content-hash\x00" records, making the result independent of
// directory listing order and OS path separators.
package contenthash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Prefix labels the algorithm so future schemes can coexist.
const Prefix = "sha256:"

// File returns "sha256:<hex>" over the file contents.
func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return Prefix + hex.EncodeToString(h.Sum(nil)), nil
}

// Bytes returns the scheme-qualified digest of an in-memory byte slice
// (canonicalized config fragments lifted from shared files).
func Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return Prefix + hex.EncodeToString(sum[:])
}

// Tree returns "sha256:<hex>" over the subtree rooted at root. Symlink
// targets are hashed as text, never followed out of the tree. An empty tree
// hashes deterministically.
func Tree(root string) (string, error) {
	type rec struct {
		rel    string
		digest string
	}
	var recs []rec
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			return nil
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			recs = append(recs, rec{rel, hash([]byte("link:" + filepath.ToSlash(target)))})
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			recs = append(recs, rec{rel, hash(b)})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(recs, func(a, b int) bool { return recs[a].rel < recs[b].rel })
	h := sha256.New()
	for _, r := range recs {
		h.Write([]byte(r.rel))
		h.Write([]byte{0})
		h.Write([]byte(r.digest))
		h.Write([]byte{0})
	}
	return Prefix + hex.EncodeToString(h.Sum(nil)), nil
}

// TreeOrFile dispatches on the path type; component paths may be either.
func TreeOrFile(root string) (string, error) {
	fi, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return Tree(root)
	}
	return File(root)
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return strings.ToLower(hex.EncodeToString(sum[:]))
}
