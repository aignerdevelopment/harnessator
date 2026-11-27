// Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The byte machinery of v0.15.5 (JSONC strip, model-list parse,
// splice) touches FOREIGN files; its promises are safety nets, and a
// safety net that panics is a hole. These fuzz properties run their
// seed corpus under plain `go test` (CI) and can be pushed with
// `go test -fuzz` when the machinery changes.

func FuzzStripJSONC(f *testing.F) {
	f.Add([]byte(`{"a": 1} // trailing`))
	f.Add([]byte(`{"u":"http://x//y" /* block */ }`))
	f.Add([]byte(`{"a":[1,2,],}`))
	f.Add([]byte(`"unterminated string`))
	f.Add([]byte(`{`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, raw []byte) {
		s := stripJSONC(raw)
		if s2 := stripJSONC(s); !reflect.DeepEqual(s, s2) {
			t.Fatalf("strip not idempotent: %q -> %q -> %q", raw, s, s2)
		}
		if json.Valid(raw) {
			var a, b any
			if json.Unmarshal(raw, &a) == nil && json.Unmarshal(s, &b) == nil && !reflect.DeepEqual(a, b) {
				t.Fatalf("strip changed a valid JSON document: %q", raw)
			}
		}
	})
}

func FuzzParseEnvelope(f *testing.F) {
	f.Add([]byte(`{"data":[{"id":"a"}]}`))
	f.Add([]byte(`{"models":["scalar-ish"]}`))
	f.Add([]byte(`{"data":null,"has_more":true,"next_url":{"not":"a string"}}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{`))
	f.Fuzz(func(t *testing.T, body []byte) {
		ids, hints, err := parseEnvelope(body)
		if err != nil {
			return // loud refusal is a valid outcome
		}
		if len(ids) > 0 && hints.hasMore == false && hints.next != "" {
			// a promise with a link is coherent; anything parseable
			// must never panic or invent ids from nothing
			for _, id := range ids {
				if id == "" {
					t.Fatalf("empty id survived the parser: %q", body)
				}
			}
		}
	})
}

func FuzzSpliceModels(f *testing.F) {
	f.Add([]byte(`{"providers":{"gw":{"package":"p","settings":{},"models":{"b":{},"keep":{"name":"K"}}}}}`))
	f.Add([]byte(`{"provider":{"gw":{"models":[]}}}`))
	f.Add([]byte(`{"providers":{"gw":{"models":{"b":"scalar"}}}}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(`{ "providers" : { } } // comment`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		out := spliceModels(raw, "gw", []string{"fuzznew"}, []string{"fuzzgone"})
		if out == nil {
			return // the documented fallback decision
		}
		var doc map[string]any
		if err := json.Unmarshal(stripJSONC(out), &doc); err != nil {
			t.Fatalf("splicer emitted unparseable bytes: %v\ninput %q\noutput %q", err, raw, out)
		}
		pm := providersOf(doc)
		if pm == nil {
			t.Fatalf("spliced output lost the providers object: %q", out)
		}
		if ent, ok := pm["gw"].(map[string]any); ok {
			if mm, ok := ent["models"].(map[string]any); ok {
				if _, gone := mm["fuzzgone"]; gone {
					t.Fatalf("removed id survived the splice: %q", out)
				}
				if _, added := mm["fuzznew"]; !added {
					t.Fatalf("added id missing from the splice: %q", out)
				}
			}
		}
	})
}
