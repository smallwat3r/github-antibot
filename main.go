package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	baseURL = "https://api.github.com"
)

type Config struct {
	Username                    string
	PAT                         string
	Threshold                   int
	Whitelist                   map[string]struct{}
	ConcurrentRequestsSemaphore chan struct{}
}

var config Config

type User struct {
	Login     string `json:"login"`
	Following int    `json:"following"`
}

func parseConfig(ghUsername, ghPat, thresholdStr, whitelistStr string) (Config, error) {
	if ghUsername == "" {
		return Config{}, fmt.Errorf("GH_USERNAME environment variable is required")
	}
	if ghPat == "" {
		return Config{}, fmt.Errorf("GH_PAT environment variable is required")
	}
	if thresholdStr == "" {
		thresholdStr = "20000"
	}
	antibotThreshold, err := strconv.Atoi(thresholdStr)
	if err != nil {
		log.Printf("Invalid ANTIBOT_THRESHOLD value, using default of 20000.")
		antibotThreshold = 20000
	}

	whitelistItems := strings.Split(whitelistStr, ",")
	antibotWhitelist := make(map[string]struct{})
	for _, item := range whitelistItems {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			antibotWhitelist[trimmed] = struct{}{}
		}
	}

	return Config{
		Username:                    ghUsername,
		PAT:                         ghPat,
		Threshold:                   antibotThreshold,
		Whitelist:                   antibotWhitelist,
		ConcurrentRequestsSemaphore: make(chan struct{}, 50),
	}, nil
}

func loadConfig() {
	ghUsername := os.Getenv("GH_USERNAME")
	ghPat := os.Getenv("GH_PAT")
	thresholdStr := os.Getenv("ANTIBOT_THRESHOLD")
	whitelistStr := os.Getenv("ANTIBOT_WHITELIST")

	var err error
	config, err = parseConfig(ghUsername, ghPat, thresholdStr, whitelistStr)
	if err != nil {
		log.Fatal(err)
	}
}

func request(method, endpoint string) (*http.Response, error) {
	config.ConcurrentRequestsSemaphore <- struct{}{}
	defer func() { <-config.ConcurrentRequestsSemaphore }()

	client := &http.Client{Timeout: time.Second * 10}

	fullURL := baseURL + endpoint

	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.PAT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", config.Username)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %s: %s", resp.Status, body)
	}

	return resp, nil
}

func getFollowers() ([]User, error) {
	var followers []User
	endpoint := fmt.Sprintf("/users/%s/followers?per_page=100", config.Username)

	for endpoint != "" {
		resp, err := request("GET", endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to get followers page: %w", err)
		}
		defer resp.Body.Close()

		var pageFollowers []User
		if err := json.NewDecoder(resp.Body).Decode(&pageFollowers); err != nil {
			return nil, fmt.Errorf("failed to decode followers: %w", err)
		}
		followers = append(followers, pageFollowers...)

		linkHeader := resp.Header.Get("Link")
		endpoint = "" // reset for next iteration
		if linkHeader == "" {
			continue
		}

		links := strings.Split(linkHeader, ",")
		for _, link := range links {
			if strings.Contains(link, `rel="next"`) {
				parts := strings.Split(link, ";")
				nextURLStr := strings.Trim(parts[0], "<> ")
				nextURL, err := url.Parse(nextURLStr)
				if err != nil {
					log.Printf("Warning: failed to parse next page URL: %v", err)
					continue
				}
				endpoint = nextURL.RequestURI()
				break
			}
		}
	}
	return followers, nil
}

func getFollowingCount(username string) (int, error) {
	endpoint := fmt.Sprintf("/users/%s", username)
	resp, err := request("GET", endpoint)
	if err != nil {
		return 0, fmt.Errorf("failed to get user details for %s: %w", username, err)
	}
	defer resp.Body.Close()

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return 0, fmt.Errorf("failed to decode user details for %s: %w", username, err)
	}
	return user.Following, nil
}

func blockUser(username string) error {
	endpoint := fmt.Sprintf("/user/blocks/%s", username)
	resp, err := request("PUT", endpoint)
	if err != nil {
		return fmt.Errorf("failed to block user %s: %w", username, err)
	}
	defer resp.Body.Close()
	log.Printf("Blocked user: %s", username)
	return nil
}

func processFollowers(followers []User) {
	var wg sync.WaitGroup
	jobs := make(chan string, len(followers))
	results := make(chan bool, len(followers))

	numWorkers := 50
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go worker(&wg, jobs, results)
	}

	for _, follower := range followers {
		jobs <- follower.Login
	}
	close(jobs)

	wg.Wait()
	close(results)

	blockedCount := 0
	for range results {
		blockedCount++
	}

	log.Printf("Finished. Blocked %d users.", blockedCount)
}

func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- bool) {
	defer wg.Done()
	for username := range jobs {
		if _, isWhitelisted := config.Whitelist[username]; isWhitelisted {
			log.Printf("Skipping whitelisted user: %s", username)
			continue
		}

		followingCount, err := getFollowingCount(username)
		if err != nil {
			log.Printf("Could not determine following count for %s: %v", username, err)
			continue
		}
		log.Printf("User %s is following %d users.", username, followingCount)

		if followingCount >= config.Threshold {
			log.Printf("User %s is following %d users, which is over the threshold of %d. Blocking.", username, followingCount, config.Threshold)
			if err := blockUser(username); err != nil {
				log.Printf("Failed to block user %s: %v", username, err)
			} else {
				results <- true
			}
		}
	}
}

func main() {
	loadConfig()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Fetching followers for %s...", config.Username)

	followers, err := getFollowers()
	if err != nil {
		log.Fatalf("Failed to fetch followers: %v", err)
	}
	log.Printf("Found %d followers.", len(followers))

	processFollowers(followers)
}
