from http import HTTPMethod
from logging import getLogger
from os import getenv
from typing import Any
from urllib.parse import parse_qs, urljoin, urlparse

import requests
from requests import Response

logger = getLogger(__name__)

GH_USERNAME = getenv("GH_USERNAME")
GH_TOKEN = getenv("GH_TOKEN")
THRESHOLD = int(getenv("ANTIBOT_THRESHOLD"))


class Github:
    _HOST = "https://api.github.com"

    def __init__(self) -> None:
        self.headers = {
            "Authorization": f"Bearer {GH_TOKEN}",
            "Accept": "application/vnd.github+json",
            "X-Github-Api-Version": "2022-11-28",
            "User-Agent": GH_USERNAME,
        }

    def _make_request(self, method: HTTPMethod, endpoint: str,
                      **kwargs) -> Response:
        response = getattr(requests, method.lower())(
            url=urljoin(self._HOST, endpoint), headers=self.headers, **kwargs)
        response.raise_for_status()
        return response

    def get_number_of_followings(self, username: str) -> int:
        response = self._make_request(
            HTTPMethod.HEAD, f"/users/{username}/following",
            # 1 result per page will ensure the next link indicates the total
            # number of users this user follows.
            params={"page": 1, "per_page": 1})
        if last := response.links.get("last"):
            url = urlparse(last["url"])
            return parse_qs(url.query)["page"][0]
        return 0

    def get_followers(self) -> dict[str, Any]:
        def paginate(next: str | None) -> dict[str, Any]:
            response = self._make_request(HTTPMethod.GET, next)
            data.append(response.json())
            if next := response.links.get("next"):
                return paginate(next)
            return data
        data = []
        return paginate(f"/users/{GH_USERNAME}/followers?page=1&per_page=100")

    def block_user(self, username: str) -> None:
        self._make_request(HTTPMethod.PUT, f"/user/blocks/{username}")


def entrypoint() -> None:
    gh = Github()
    for follower in gh.get_followers():
        username = follower["login"]
        nb_of_followings = gh.get_number_of_followings(username)
        if nb_of_followings >= THRESHOLD:
            logger.info("Blocking user %s which follows %s users", username,
                        nb_of_followings)
            gh.block_user(username)


if __name__ == "__main__":
    entrypoint()
