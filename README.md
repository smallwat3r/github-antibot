# Block bot abusers

A bot to block bot abusers. I got tired of receiving notifications of users following me, that are using bots to perform massive "follow waves" in order to gain more followers. This repository runs a Github action every day to block users that follow me and follow already more than 20,000 users (which to me, are probably users using bots). No added value into keeping them on my Github.

You will need to create a Github PAT token with the following permissions:
- Read-only - Followers
- Read and write - Block another user

Then stick the PAT token value as a secret in the repository containing this action, under the name `GH_PAT`.
