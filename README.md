# Block bot abusers

Tired of spammy notifications from mass-following bot accounts? This repository contains a GitHub Action that automatically blocks suspicious users who follow you — particularly those who follow tens of thousands of other accounts just to boost their own follower count.

This action runs daily and checks your followers. If a user is following more than a defined threshold (default: 20,000), the action considers them likely a bot and blocks them. These accounts typically provide no value and clutter your followers list.

To use this action, you'll need to create a [**Github PAT token**](https://github.com/settings/personal-access-tokens) with the following settings:
- Repository access:
    - Only select repositories (select this repository)
- Permissions:
    - Repository permissions:
        - Contents: Read and Write
    - Account permissions:
        - Block another user: Read and write
        - Followers: Read-only 

Add this token as a repository secret named `GH_PAT`.

## Configuration

The action supports the following environment variables for configuration:

- `GH_USERNAME`: **Required.** Your GitHub username.
- `GH_PAT`: **Required.** Your GitHub Personal Access Token.
- `ANTIBOT_THRESHOLD`: The number of people a user must be following to be considered a bot (default: `20000`).
- `ANTIBOT_WHITELIST`: A comma-separated list of usernames to exclude from blocking, even if they exceed the threshold.

**Note:** The application also enforces a concurrent request limit of 50 to comply with GitHub's API restrictions (<100).

GitHub Actions may stop running scheduled workflows for inactive repositories. To prevent this, each pipeline run updates the `.keep_alive` file with a new UUID and commits the change, ensuring continued activity and keeping the workflow alive over time.
