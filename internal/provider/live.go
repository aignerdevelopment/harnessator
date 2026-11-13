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

// Probe lists the model ids reachable at one style's endpoint.
func Probe(ctx context.Context, style string, ep Endpoint, key string) ([]string, error) {
	base := strings.TrimSuffix(ep.BaseURL, "/")
	path := "/models"
	if style == StyleAnthropicCompat {
		path = "/v1/models"
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
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
		return nil, fmt.Errorf("cannot reach %s gateway at %s - check the URL, the network and the proxy", style, base)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("HTTP %d: the gateway rejected the key - check the VALUE of %s (right gateway, right key?)", resp.StatusCode, ep.AuthEnv)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("HTTP %d from %s%s", resp.StatusCode, base, path)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return parseModelIDs(body)
}

type modelEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// parseModelIDs accepts the known list envelopes; unknown shapes error
// loudly rather than pretending an empty catalog is "reachable".
func parseModelIDs(body []byte) ([]string, error) {
	var env struct {
		Data   []modelEntry `json:"data"`
		Models []modelEntry `json:"models"`
	}
	if err := json.Unmarshal(body, &env); err == nil && (env.Data != nil || env.Models != nil) {
		all := append(append([]modelEntry{}, env.Data...), env.Models...)
		return idsOf(all), nil
	}
	var bare []modelEntry
	if err := json.Unmarshal(body, &bare); err == nil {
		return idsOf(bare), nil
	}
	t := strings.TrimSpace(string(body))
	if len(t) > 60 {
		t = t[:60]
	}
	return nil, fmt.Errorf("unrecognized model-list response (starts %q)", t)
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
