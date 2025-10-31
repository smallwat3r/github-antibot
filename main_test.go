package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func setup() (server *httptest.Server, mux *http.ServeMux, teardown func()) {
	mux = http.NewServeMux()
	server = httptest.NewServer(mux)

	baseURL = server.URL
	config.ConcurrentRequestsSemaphore = make(chan struct{}, 50)
	teardown = func() {
		server.Close()
	}
	return
}

func TestParseConfig(t *testing.T) {
	t.Run("all variables set", func(t *testing.T) {
		cfg, err := parseConfig("testuser", "testpat", "100", "user1, user2")
		if err != nil {
			t.Fatalf("parseConfig() returned error: %v", err)
		}

		expected := Config{
			Username:  "testuser",
			PAT:       "testpat",
			Threshold: 100,
			Whitelist: map[string]struct{}{"user1": {}, "user2": {}},
		}

		// ignore ConcurrentRequestsSemaphore for comparison
		cfg.ConcurrentRequestsSemaphore = nil
		if !reflect.DeepEqual(cfg, expected) {
			t.Errorf("parseConfig() = %+v, want %+v", cfg, expected)
		}
	})

	t.Run("default values", func(t *testing.T) {
		cfg, err := parseConfig("testuser", "testpat", "", "")
		if err != nil {
			t.Fatalf("parseConfig() returned error: %v", err)
		}

		if cfg.Threshold != 20000 {
			t.Errorf("Expected Threshold to be 20000, got %d", cfg.Threshold)
		}
		if len(cfg.Whitelist) != 0 {
			t.Errorf("Expected Whitelist to be empty, got %v", cfg.Whitelist)
		}
	})

	t.Run("missing required values", func(t *testing.T) {
		_, err := parseConfig("", "testpat", "", "")
		if err == nil {
			t.Error("Expected error for missing username, got nil")
		}
		_, err = parseConfig("testuser", "", "", "")
		if err == nil {
			t.Error("Expected error for missing PAT, got nil")
		}
	})
}

func TestGetFollowers(t *testing.T) {
	server, mux, teardown := setup()
	defer teardown()

	config.Username = "testuser"
	config.PAT = "testpat"
	config.ConcurrentRequestsSemaphore = make(chan struct{}, 50)

	mux.HandleFunc("/users/testuser/followers", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("page") == "" {
			linkURL := fmt.Sprintf("<%s/users/testuser/followers?page=2&per_page=100>; rel=\"next\"", server.URL)
			w.Header().Set("Link", linkURL)
			fmt.Fprint(w, `[{"login": "user1"}, {"login": "user2"}]`)
			return
		}
		if q.Get("page") == "2" {
			fmt.Fprint(w, `[{"login": "user3"}]`)
			return
		}
	})

	followers, err := getFollowers()
	if err != nil {
		t.Fatalf("getFollowers() returned error: %v", err)
	}

	expected := []User{{Login: "user1"}, {Login: "user2"}, {Login: "user3"}}
	if !reflect.DeepEqual(followers, expected) {
		t.Errorf("getFollowers() = %v, want %v", followers, expected)
	}
}

func TestGetFollowingCount(t *testing.T) {
	_, mux, teardown := setup()
	defer teardown()

	config.ConcurrentRequestsSemaphore = make(chan struct{}, 50)

	mux.HandleFunc("/users/testuser", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"login": "testuser", "following": 42}`)
	})

	count, err := getFollowingCount("testuser")
	if err != nil {
		t.Fatalf("getFollowingCount() returned error: %v", err)
	}

	if count != 42 {
		t.Errorf("getFollowingCount() = %d, want 42", count)
	}
}

func TestBlockUser(t *testing.T) {
	_, mux, teardown := setup()
	defer teardown()

	config.ConcurrentRequestsSemaphore = make(chan struct{}, 50)

	mux.HandleFunc("/user/blocks/baduser", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT request, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	err := blockUser("baduser")
	if err != nil {
		t.Fatalf("blockUser() returned error: %v", err)
	}
}

func TestProcessFollowers(t *testing.T) {
	server, mux, teardown := setup()
	defer teardown()

	config = Config{
		Username:                    "testuser",
		PAT:                         "testpat",
		Threshold:                   100,
		Whitelist:                   map[string]struct{}{"gooduser": {}},
		ConcurrentRequestsSemaphore: make(chan struct{}, 50),
	}
	baseURL = server.URL

	blockedUsersChan := make(chan string, 3)

	// mock for getFollowingCount
	mux.HandleFunc("/users/highfollowing", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"login": "highfollowing", "following": 200}`)
	})
	mux.HandleFunc("/users/lowfollowing", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"login": "lowfollowing", "following": 50}`)
	})
	mux.HandleFunc("/users/gooduser", func(w http.ResponseWriter, r *http.Request) {
		t.Error("getFollowingCount called for whitelisted user")
	})

	// mock for blockUser
	mux.HandleFunc("/user/blocks/highfollowing", func(w http.ResponseWriter, r *http.Request) {
		blockedUsersChan <- "highfollowing"
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/user/blocks/lowfollowing", func(w http.ResponseWriter, r *http.Request) {
		t.Error("blockUser called for user with low following count")
		w.WriteHeader(http.StatusNoContent)
	})

	followers := []User{
		{Login: "highfollowing"},
		{Login: "lowfollowing"},
		{Login: "gooduser"},
	}

	processFollowers(followers)

	close(blockedUsersChan)

	var blockedUsers []string
	for user := range blockedUsersChan {
		blockedUsers = append(blockedUsers, user)
	}

	expectedBlocked := []string{"highfollowing"}
	if !reflect.DeepEqual(blockedUsers, expectedBlocked) {
		t.Errorf("processFollowers() blocked %v, want %v", blockedUsers, expectedBlocked)
	}
}
