package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultThreshold = 20000
	githubBaseURL    = "https://api.github.com"
	apiVersion       = "2022-11-28"
)

type Config struct {
	Username  string
	PAT       string
	Threshold int
	Whitelist map[string]struct{} // lowercased logins
}

func ParseConfig(ghUsername, ghPat, thresholdStr, whitelistStr string) (Config, error) {
	if ghUsername == "" {
		return Config{}, errors.New("GH_USERNAME is required")
	}
	if ghPat == "" {
		return Config{}, errors.New("GH_PAT is required")
	}
	threshold := defaultThreshold
	if v, err := strconv.Atoi(strings.TrimSpace(thresholdStr)); err == nil && v > 0 {
		threshold = v
	}
	return Config{
		Username:  ghUsername,
		PAT:       ghPat,
		Threshold: threshold,
		Whitelist: parseWhitelist(whitelistStr),
	}, nil
}

// parseWhitelist lowercases entries, GitHub logins are case insensitive.
func parseWhitelist(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for item := range strings.SplitSeq(s, ",") {
		if v := strings.ToLower(strings.TrimSpace(item)); v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}

type GitHubClient struct {
	BaseURL   string
	HTTP      *http.Client
	Token     string
	UserAgent string
}

func NewGitHubClient(token, userAgent string) *GitHubClient {
	return &GitHubClient{
		BaseURL:   githubBaseURL,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		Token:     token,
		UserAgent: userAgent,
	}
}

func (c *GitHubClient) do(
	ctx context.Context,
	method, endpoint string,
	body io.Reader,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, fmt.Errorf(
			"github request returned status %s: %s",
			resp.Status,
			strings.TrimSpace(string(b)),
		)
	}
	return resp, nil
}

type User struct {
	Login     string
	Following int
}

// followersQuery returns followers together with their following count, so
// there is no per-follower lookup afterwards.
const followersQuery = `query($login: String!, $after: String) {
  user(login: $login) {
    followers(first: 100, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes { login following { totalCount } }
    }
  }
}`

type followersResponse struct {
	Data struct {
		User struct {
			Followers struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []struct {
					Login     string `json:"login"`
					Following struct {
						TotalCount int `json:"totalCount"`
					} `json:"following"`
				} `json:"nodes"`
			} `json:"followers"`
		} `json:"user"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *GitHubClient) GetFollowers(ctx context.Context, username string) ([]User, error) {
	var out []User
	var after *string

	for {
		payload, err := json.Marshal(map[string]any{
			"query":     followersQuery,
			"variables": map[string]any{"login": username, "after": after},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to encode query: %w", err)
		}
		resp, err := c.do(ctx, http.MethodPost, "/graphql", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("failed to get followers page: %w", err)
		}

		var page followersResponse
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to decode followers: %w", err)
		}
		if len(page.Errors) > 0 {
			return nil, fmt.Errorf("graphql error: %s", page.Errors[0].Message)
		}

		followers := page.Data.User.Followers
		for _, n := range followers.Nodes {
			out = append(out, User{Login: n.Login, Following: n.Following.TotalCount})
		}
		if !followers.PageInfo.HasNextPage {
			return out, nil
		}
		cursor := followers.PageInfo.EndCursor
		after = &cursor
	}
}

func (c *GitHubClient) BlockUser(ctx context.Context, username string) error {
	resp, err := c.do(ctx, http.MethodPut, "/user/blocks/"+url.PathEscape(username), nil)
	if err != nil {
		return fmt.Errorf("failed to block %s: %w", username, err)
	}
	resp.Body.Close()
	return nil
}

func ProcessFollowers(ctx context.Context, gh *GitHubClient, cfg Config, followers []User) int {
	blocked := 0
	for _, f := range followers {
		if _, ok := cfg.Whitelist[strings.ToLower(f.Login)]; ok {
			log.Printf("skip whitelisted: %s", f.Login)
			continue
		}
		if f.Following < cfg.Threshold {
			continue
		}
		log.Printf("blocking %s: following %d >= threshold %d", f.Login, f.Following, cfg.Threshold)
		if err := gh.BlockUser(ctx, f.Login); err != nil {
			log.Printf("block %s failed: %v", f.Login, err)
			continue
		}
		blocked++
	}
	return blocked
}

func main() {
	cfg, err := ParseConfig(
		os.Getenv("GH_USERNAME"),
		os.Getenv("GH_PAT"),
		os.Getenv("ANTIBOT_THRESHOLD"),
		os.Getenv("ANTIBOT_WHITELIST"),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	gh := NewGitHubClient(cfg.PAT, cfg.Username)

	log.Printf("fetching followers for %s...", cfg.Username)
	followers, err := gh.GetFollowers(ctx, cfg.Username)
	if err != nil {
		log.Fatalf("failed to fetch followers: %v", err)
	}
	log.Printf("found %d followers", len(followers))

	blocked := ProcessFollowers(ctx, gh, cfg, followers)
	log.Printf("finished. blocked %d users.", blocked)
}
