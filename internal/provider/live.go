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

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Live model discovery: the network half of "is this catalog true?".
// Probe asks one style's endpoint for its model list and returns the
// reachable ids - the input to tune's catalog reconciliation. Auth
// secrets are read from the environment BY THE CALLER and passed in;
// nothing here logs or echoes a value, and error strings name the
// variable, never its content.
//
// Endpoint shapes follow the two documented gateway conventions:
//   - openai-compatible / openai-responses: GET <base>/models,
//     "Authorization: Bearer $KEY"
//   - anthropic-compatible: GET <base>/v1/models, "x-api-key" plus
//     "anthropic-version"
//
// Envelope parsing is deliberately lenient ({data:[{id}]}, {models:[{id}]},
// or a bare list) because gateways in the wild ship all three.

const probeTimeout = 8 * time.Second

// Probe lists the model ids reachable at one style's endpoint, ONE
// page. Callers that draw conclusions from ABSENT ids (modelupdate's
// removals) must use ProbeAll instead - page one is not the truth of
// a paginated catalog.
func Probe(ctx context.Context, style string, ep Endpoint, key string) ([]string, error) {
	base := strings.TrimSuffix(ep.BaseURL, "/")
	body, err := probeGet(ctx, style, ep, key, base+probePath(style))
	if err != nil {
		return nil, err
	}
	return parseModelIDs(body)
}

func probePath(style string) string {
	if style == StyleAnthropicCompat {
		return "/v1/models"
	}
	return "/models"
}

func probeGet(ctx context.Context, style string, ep Endpoint, key, fullURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	if style == StyleAnthropicCompat {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s gateway - check the URL, the network and the proxy (%s)", style, strings.TrimSuffix(ep.BaseURL, "/"))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		if ep.AuthEnv == "" || ep.AuthEnv == "none" {
			return nil, fmt.Errorf("HTTP %d: the gateway refused the request and no credential source is configured (set auth_env in the profile, or export the key)", resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP %d: the gateway rejected the key - check the VALUE of %s (right gateway, right key?)", resp.StatusCode, ep.AuthEnv)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, fullURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

const (
	paginateMaxPages = 64
	paginateMaxIDs   = 20000
)

// ProbeAll walks the gateway's pagination hints and returns the full
// id set plus whether the enumeration is COMPLETE. Incomplete means:
// a hint promised more but no followable link was given, a cursor
// repeat was detected, or a safety cap was hit. The doctrine this
// serves: an id that was never enumerated is UNKNOWN, not deprecated -
// consumers may add from an incomplete set but must not remove on it.
// Supported hint shapes: next_url / next (URL strings), and
// has_more + next_cursor (OpenAI-style cursor param `after`).
func ProbeAll(ctx context.Context, style string, ep Endpoint, key string) (ids []string, complete bool, err error) {
	base := strings.TrimSuffix(ep.BaseURL, "/")
	next := base + probePath(style)
	seenURL := map[string]bool{}
	have := map[string]bool{}
	for page := 1; page <= paginateMaxPages; page++ {
		if seenURL[next] {
			// cycle: the gateway loops us back - enumeration is not
			// trustworthy, and a repeating page adds nothing anyway
			return accIDs(have), false, nil
		}
		seenURL[next] = true
		body, gerr := probeGet(ctx, style, ep, key, next)
		if gerr != nil {
			if page == 1 {
				return nil, false, gerr
			}
			// page 1 worked; a later failure leaves a PARTIAL truth
			return accIDs(have), false, nil
		}
		pageIDs, hints, perr := parseEnvelope(body)
		if perr != nil {
			if page == 1 {
				return nil, false, perr
			}
			return accIDs(have), false, nil
		}
		fresh := 0
		for _, id := range pageIDs {
			if !have[id] {
				have[id] = true
				fresh++
			}
		}
		if len(have) > paginateMaxIDs {
			return accIDs(have), false, nil
		}
		if hints.next == "" && !hints.hasMore {
			return accIDs(have), true, nil // no promises left: done, complete
		}
		if hints.next != "" {
			next = resolveRef(base, hints.next)
			continue
		}
		// has_more with a cursor: OpenAI-style ?after= parameter
		if hints.cursor == "" {
			return accIDs(have), false, nil // promised more, gave no handle
		}
		if fresh == 0 {
			// the cursor gained us nothing (unsupported param?) - stop
			// claiming more without proof
			return accIDs(have), false, nil
		}
		sep := "?"
		if strings.Contains(next, "?") {
			sep = "&"
		}
		next = next + sep + "after=" + url.QueryEscape(hints.cursor)
	}
	return accIDs(have), false, nil
}

func accIDs(have map[string]bool) []string {
	out := make([]string, 0, len(have))
	for id := range have {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func resolveRef(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if !strings.HasPrefix(ref, "/") {
		ref = "/" + ref
	}
	return base + ref
}

type modelEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// parseModelIDs accepts the known list envelopes; unknown shapes error
// loudly rather than pretending an empty catalog is "reachable".
func parseModelIDs(body []byte) ([]string, error) {
	ids, _, err := parseEnvelope(body)
	return ids, err
}

type listHints struct {
	next    string
	hasMore bool
	cursor  string
}

func parseEnvelope(body []byte) ([]string, listHints, error) {
	var env struct {
		Data       []modelEntry `json:"data"`
		Models     []modelEntry `json:"models"`
		Next       any          `json:"next"`
		NextURL    string       `json:"next_url"`
		HasMore    *bool        `json:"has_more"`
		NextCursor string       `json:"next_cursor"`
	}
	h := listHints{}
	if err := json.Unmarshal(body, &env); err == nil && (env.Data != nil || env.Models != nil) {
		all := append(append([]modelEntry{}, env.Data...), env.Models...)
		h.next = env.NextURL
		if h.next == "" {
			if s, ok := env.Next.(string); ok {
				h.next = s
			}
		}
		if env.HasMore != nil {
			h.hasMore = *env.HasMore
		}
		h.cursor = env.NextCursor
		return idsOf(all), h, nil
	}
	var bare []modelEntry
	if err := json.Unmarshal(body, &bare); err == nil {
		return idsOf(bare), h, nil
	}
	t := strings.TrimSpace(string(body))
	if len(t) > 60 {
		t = t[:60]
	}
	return nil, h, fmt.Errorf("unrecognized model-list response (starts %q)", t)
}

func idsOf(es []modelEntry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range es {
		id := e.ID
		if id == "" {
			id = e.Name
		}
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
