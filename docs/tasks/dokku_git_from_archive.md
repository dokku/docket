# dokku_git_from_archive

Deploys a git repository from an archive URL

## Deploy a tar archive

```yaml
dokku_git_from_archive:
    app: node-js-app
    archive_url: https://example.com/release-1.0.0.tar
```

## Deploy a zip archive with author metadata

```yaml
dokku_git_from_archive:
    app: node-js-app
    archive_url: https://example.com/release-1.0.0.zip
    archive_type: zip
    git_username: deploy-bot
    git_email: deploy@example.com
```
