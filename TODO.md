# TODO

- Support private GitHub and GitLab repositories. API tokens are already supported (`github.token_*`, `gitlab_hosts`), but the token is deliberately never sent with asset downloads, so assets of private releases cannot be fetched. Needs: sending the token on the asset download for the configured host only (dropped on cross-host redirects), plus a per-repo way to pass a token.
