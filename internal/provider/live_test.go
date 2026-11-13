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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeOpenAICompat(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		fmt.Fprint(w, `{"data":[{"id":"gpt-5.3"},{"id":"claude-sonnet-5"},{"id":"gpt-5.3"}]}`)
	}))
	defer srv.Close()

	ids, err := Probe(context.Background(), StyleOpenAICompat,
		Endpoint{BaseURL: srv.URL + "/v1", AuthEnv: "K"}, "sekret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "claude-sonnet-5,gpt-5.3" {
		t.Errorf("ids = %v (want sorted+deduped)", ids)
	}
	if gotAuth != "Bearer sekret" || gotPath != "/v1/models" {
		t.Errorf("got auth %q path %q", gotAuth, gotPath)
	}
}

func TestProbeAnthropicCompat(t *testing.T) {
	var gotKey, gotVer, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotVer, gotPath = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"), r.URL.Path
		fmt.Fprint(w, `{"models":[{"id":"claude-opus-5"}]}`) // bare-list-in-key shape
	}))
	defer srv.Close()

	ids, err := Probe(context.Background(), StyleAnthropicCompat,
		Endpoint{BaseURL: srv.URL + "/anthropic/", AuthEnv: "A"}, "key")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "claude-opus-5" {
		t.Errorf("ids = %v", ids)
	}
	// trailing slash trimmed, /v1/models appended, headers per convention
	if gotPath != "/anthropic/v1/models" || gotKey != "key" || gotVer != "2023-06-01" {
		t.Errorf("path %q key %q ver %q", gotPath, gotKey, gotVer)
	}
}

func TestProbeErrorsAreHelpful(t *testing.T) {
	srv401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv401.Close()
	_, err := Probe(context.Background(), StyleOpenAICompat,
		Endpoint{BaseURL: srv401.URL, AuthEnv: "MY_KEY"}, "wrong")
	if err == nil || !strings.Contains(err.Error(), "VALUE of MY_KEY") {
		t.Errorf("want key-rejection naming MY_KEY, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "wrong") {
		t.Error("error leaked the secret value")
	}

	_, err = Probe(context.Background(), StyleOpenAICompat,
		Endpoint{BaseURL: "http://127.0.0.1:1", AuthEnv: "K"}, "x")
	if err == nil || !strings.Contains(err.Error(), "cannot reach") {
		t.Errorf("want unreachable hint, got %v", err)
	}
}
