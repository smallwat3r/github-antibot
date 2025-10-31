package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func setup() (server *httptest.Server, mux *http.ServeMux, teardown func()) {
	mux = http.NewServeMux()
	server = httptest.NewServer(mux)

	baseURL = server.URL
	teardown = func() {
		server.Close()
		// reset baseURL
		baseURL = "https://api.github.com"
	}
	return
}

func TestLoadConfig(t *testing.T) {
	// backup original env vars
	originalGHUsername := os.Getenv("GH_USERNAME")
	originalGHPAT := os.Getenv("GH_PAT")
	originalThreshold := os.Getenv("ANTIBOT_THRESHOLD")
	originalWhitelist := os.Getenv("ANTIBOT_WHITELIST")
	// defer restoring original env vars
	defer func() {
		os.Setenv("GH_USERNAME", originalGHUsername)
		os.Setenv("GH_PAT", originalGHPAT)
		os.Setenv("ANTIBOT_THRESHOLD", originalThreshold)
		os.Setenv("ANTIBOT_WHITELIST", originalWhitelist)
	}()

	t.Run("all variables set", func(t *testing.T) {
		os.Setenv("GH_USERNAME", "testuser")
		os.Setenv("GH_PAT", "testpat")
		os.Setenv("ANTIBOT_THRESHOLD", "100")
		os.Setenv("ANTIBOT_WHITELIST", "user1, user2")

		loadConfig()

		expected := Config{
			Username:  "testuser",
			PAT:       "testpat",
			Threshold: 100,
			Whitelist: map[string]struct{}{"user1": {}, "user2": {}},
		}

		if !reflect.DeepEqual(config, expected) {
			t.Errorf("loadConfig() = %+v, want %+v", config, expected)
		}
	})

	t.Run("default values", func(t *testing.T) {
		os.Setenv("GH_USERNAME", "testuser")
		os.Setenv("GH_PAT", "testpat")
		os.Setenv("ANTIBOT_THRESHOLD", "")
		os.Setenv("ANTIBOT_WHITELIST", "")

		loadConfig()

		if config.Threshold != 20000 {
			t.Errorf("Expected Threshold to be 20000, got %d", config.Threshold)
		}
		if len(config.Whitelist) != 0 {
			t.Errorf("Expected Whitelist to be empty, got %v", config.Whitelist)
		}
	})
}

func TestRequest(t *testing.T) {
	_, mux, teardown := setup()
	defer teardown()

	config.Username = "testuser"
	config.PAT = "testpat"

	mux.HandleFunc("/test-endpoint", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected method GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer testpat" {
			t.Errorf("Wrong Authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Wrong Accept header: %s", r.Header.Get("Accept"))
		}
		if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Errorf("Wrong X-GitHub-Api-Version header: %s", r.Header.Get("X-GitHub-Api-Version"))
		}
		if r.Header.Get("User-Agent") != "testuser" {
			t.Errorf("Wrong User-Agent header: %s", r.Header.Get("User-Agent"))
		}
		fmt.Fprint(w, "ok")
	})

	mux.HandleFunc("/test-error", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	})

	t.Run("successful request", func(t *testing.T) {
		resp, err := request("GET", "/test-endpoint", "")
		if err != nil {
			t.Fatalf("request() returned error: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 OK, got %s", resp.Status)
		}
	})

	t.Run("failed request", func(t *testing.T) {
		_, err := request("GET", "/test-error", "")
		if err == nil {
			t.Fatal("request() did not return error for failed request")
		}
	})

	t.Run("custom base url", func(t *testing.T) {
		// separate server for custom url
		mux2 := http.NewServeMux()
		server2 := httptest.NewServer(mux2)
		defer server2.Close()

		mux2.HandleFunc("/custom-endpoint", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "ok from custom")
		})

		resp, err := request("GET", "/custom-endpoint", server2.URL)
		if err != nil {
			t.Fatalf("request() with custom URL returned error: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "ok from custom" {
			t.Errorf("Expected body 'ok from custom', got '%s'", string(body))
		}
	})
}

func TestGetFollowers(t *testing.T) {
	server, mux, teardown := setup()
	defer teardown()

	config.Username = "testuser"
	config.PAT = "testpat"

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
		Username:  "testuser",
		PAT:       "testpat",
		Threshold: 100,
		Whitelist: map[string]struct{}{"gooduser": {}},
	}
	baseURL = server.URL

	var blockedUsers []string

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
		blockedUsers = append(blockedUsers, "highfollowing")
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

	expectedBlocked := []string{"highfollowing"}
	if !reflect.DeepEqual(blockedUsers, expectedBlocked) {
		t.Errorf("processFollowers() blocked %v, want %v", blockedUsers, expectedBlocked)
	}
}
