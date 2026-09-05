package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	t.Parallel()

	cfg, err := ParseConfig("alice", "topsecret", "", "Bob,  carol ,  ")
	if err != nil {
		t.Fatalf("ParseConfig error: %v", err)
	}
	if cfg.Threshold != defaultThreshold {
		t.Errorf("Threshold = %d, want default %d", cfg.Threshold, defaultThreshold)
	}
	wantWL := map[string]struct{}{"bob": {}, "carol": {}}
	if !reflect.DeepEqual(cfg.Whitelist, wantWL) {
		t.Errorf("Whitelist = %#v, want %#v", cfg.Whitelist, wantWL)
	}

	cfg, err = ParseConfig("u", "t", "123", "")
	if err != nil || cfg.Threshold != 123 {
		t.Errorf("Threshold = %d (err %v), want 123", cfg.Threshold, err)
	}
	if _, err := ParseConfig("", "t", "", ""); err == nil {
		t.Error("expected error for missing username")
	}
	if _, err := ParseConfig("u", "", "", ""); err == nil {
		t.Error("expected error for missing token")
	}
}

// fakeGitHub serves a paginated GraphQL followers list and records blocks.
type fakeGitHub struct {
	mu      sync.Mutex
	pages   [][]User
	blocked map[string]bool
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			var body struct {
				Variables struct {
					After *string `json:"after"`
				} `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("bad graphql body: %v", err)
			}
			idx := 0
			if body.Variables.After != nil {
				fmt.Sscanf(*body.Variables.After, "cursor%d", &idx)
			}
			nodes := make([]map[string]any, 0)
			for _, u := range f.pages[idx] {
				nodes = append(nodes, map[string]any{
					"login":     u.Login,
					"following": map[string]int{"totalCount": u.Following},
				})
			}
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"user": map[string]any{"followers": map[string]any{
					"pageInfo": map[string]any{
						"hasNextPage": idx+1 < len(f.pages),
						"endCursor":   fmt.Sprintf("cursor%d", idx+1),
					},
					"nodes": nodes,
				}}},
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/user/blocks/"):
			name := strings.TrimPrefix(r.URL.Path, "/user/blocks/")
			if name == "flaky" {
				http.Error(w, `{"message":"boom"}`, http.StatusForbidden)
				return
			}
			f.mu.Lock()
			f.blocked[name] = true
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
}

func newFake(t *testing.T, pages ...[]User) (*fakeGitHub, *GitHubClient) {
	f := &fakeGitHub{pages: pages, blocked: map[string]bool{}}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return f, &GitHubClient{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}, Token: "t"}
}

func TestGetFollowers(t *testing.T) {
	t.Parallel()

	_, gh := newFake(t,
		[]User{{Login: "u1", Following: 1}, {Login: "u2", Following: 2}},
		[]User{{Login: "u3", Following: 3}},
	)
	got, err := gh.GetFollowers(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetFollowers error: %v", err)
	}
	want := []User{{"u1", 1}, {"u2", 2}, {"u3", 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("followers = %#v, want %#v", got, want)
	}

	t.Run("graphql error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"errors":[{"message":"bad login"}]}`))
		}))
		t.Cleanup(srv.Close)
		gh := &GitHubClient{BaseURL: srv.URL, HTTP: srv.Client(), Token: "t"}
		if _, err := gh.GetFollowers(context.Background(), "alice"); err == nil ||
			!strings.Contains(err.Error(), "bad login") {
			t.Errorf("err = %v, want graphql error", err)
		}
	})

	t.Run("http error", func(t *testing.T) {
		t.Parallel()
		gh := &GitHubClient{BaseURL: "://invalid", HTTP: http.DefaultClient, Token: "t"}
		if _, err := gh.GetFollowers(context.Background(), "alice"); err == nil {
			t.Error("expected error for invalid base URL")
		}
	})
}

func TestProcessFollowers(t *testing.T) {
	t.Parallel()

	f, gh := newFake(t)
	cfg := Config{Threshold: 20000, Whitelist: map[string]struct{}{"ok2": {}}}
	followers := []User{
		{Login: "spam1", Following: 50000},
		{Login: "spam2", Following: 20000},
		{Login: "ok1", Following: 19999},
		{Login: "OK2", Following: 50000}, // whitelisted, case insensitive
		{Login: "flaky", Following: 50000},
	}

	if got := ProcessFollowers(context.Background(), gh, cfg, followers); got != 2 {
		t.Errorf("blocked = %d, want 2", got)
	}
	want := map[string]bool{"spam1": true, "spam2": true}
	if !reflect.DeepEqual(f.blocked, want) {
		t.Errorf("blocked users = %#v, want %#v", f.blocked, want)
	}
}
