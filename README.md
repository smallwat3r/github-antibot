# GitHub Antibot

Tired of spammy notifications from mass-following bot accounts? This GitHub Action automatically blocks suspicious users who follow you, helping to keep your follower list clean and your notifications relevant.

## How It Works

1. The action runs daily (or on-demand via workflow dispatch)
2. Fetches your followers along with how many accounts each of them follows, in a single GraphQL query per 100 followers
3. Compares each following count against the threshold
4. If that count exceeds the threshold (default: 20,000), the user is blocked
5. Whitelisted users are skipped regardless of their following count

The default threshold of 20,000 is based on the observation that legitimate users rarely follow more than a few thousand accounts. Mass-following bots, on the other hand, often follow tens of thousands of accounts to trigger follow-back notifications.

## Usage

1. **Fork this repository** to your GitHub account
2. **Create a Personal Access Token** with the required permissions (see [PAT Configuration](#pat-configuration))
3. **Add the token as a repository secret** named `GH_PAT`
4. **Adjust the configuration** in [antibot.yaml](./.github/workflows/antibot.yaml) if needed
5. **Enable GitHub Actions** on your fork if not already enabled

The action will run automatically every day at midnight UTC. You can also trigger it manually from the Actions tab using "Run workflow".

## Configuration

The action [antibot.yaml](./.github/workflows/antibot.yaml) is configured using environment variables:

- `GH_PAT`: **Required.** A GitHub Personal Access Token with the necessary permissions to read followers and block users. See [PAT Configuration](#pat-configuration) for details.
- `ANTIBOT_THRESHOLD`: The number of accounts a user must be following to be considered a bot. Defaults to `20000`.
- `ANTIBOT_WHITELIST`: A comma-separated list of usernames to exclude from blocking, even if they exceed the threshold. Case insensitive.

**Note:** The `GH_USERNAME` is automatically determined from the user running the action (`github.actor`).

## PAT Configuration

You need to create a [GitHub Personal Access Token](https://github.com/settings/personal-access-tokens) with the following permissions:

- **Account permissions:**
  - `Blocking users`: Read and write
  - `Followers`: Read-only

Once created, add the token *as a repository secret* named `GH_PAT`. For instructions on how to add secrets, refer to [GitHub's documentation on encrypted secrets](https://docs.github.com/en/actions/security-guides/encrypted-secrets).

## Example Output

When the action runs, you'll see output like this in the workflow logs:

```
2024/01/15 00:00:01 fetching followers for your-username...
2024/01/15 00:00:02 found 150 followers
2024/01/15 00:00:03 skip whitelisted: trusted-user
2024/01/15 00:00:04 blocking spam-bot-123: following 45000 >= threshold 20000
2024/01/15 00:00:05 blocking mass-follower: following 32000 >= threshold 20000
2024/01/15 00:00:06 finished. blocked 2 users.
```

## Keep-Alive Mechanism

GitHub disables scheduled workflows after 60 days without repository activity. To prevent this, each run re-enables the workflow through the GitHub API, which resets that timer without creating a commit. This uses the workflow's own `GITHUB_TOKEN` with `actions: write`, no extra PAT permission is needed.
