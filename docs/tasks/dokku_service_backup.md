# dokku_service_backup

## Synopsis

Manages the S3 backup schedule, authentication, and encryption for a dokku service

## Requirements

- a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Not supported - service export is not yet implemented; tracked in issue #279.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to back up (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `schedule` | string | no |  |  | Cron schedule for backups (requires bucket) |
| `bucket` | string | no |  |  | S3 bucket backups are written to (requires schedule) |
| `use_iam` | bool | no |  |  | Use the instance IAM profile instead of static credentials |
| `aws_access_key_id` | string | no |  |  | AWS access key id for backup auth (requires aws_secret_access_key) |
| `aws_secret_access_key` | string | no |  |  | AWS secret access key for backup auth (requires aws_access_key_id) (sensitive) |
| `aws_default_region` | string | no |  |  | AWS region used for backup auth |
| `aws_signature_version` | string | no |  |  | AWS signature version used for backup auth |
| `endpoint_url` | string | no |  |  | S3-compatible endpoint url used for backup auth |
| `encryption_passphrase` | string | no |  |  | Passphrase used to encrypt future backups (sensitive) |
| `state` | string | no | present | present, absent | Desired state of the backup configuration |

## Examples

### Schedule daily backups of a postgres service to S3

```yaml
dokku_service_backup:
    service: postgres
    name: my-db
    schedule: 0 3 * * *
    bucket: my-backup-bucket
    aws_access_key_id: AKIAEXAMPLE
    aws_secret_access_key: examplesecret
    aws_default_region: us-east-1
    encryption_passphrase: correct-horse-battery-staple
```

### Schedule backups using the instance IAM profile

```yaml
dokku_service_backup:
    service: postgres
    name: my-db
    schedule: 0 3 * * *
    bucket: my-backup-bucket
    use_iam: true
```

### Remove the backup configuration for a postgres service

```yaml
dokku_service_backup:
    service: postgres
    name: my-db
    state: absent
```

## Return Values

Available after the task runs when captured with `register:`, referenced as `result.<Key>` (or `registered.<name>.<Key>`).

| Key | Returned | Type | Description |
| --- | --- | --- | --- |
| `Changed` | always | bool | Whether the task changed server state. |
| `State` | always | string | Resulting state of the resource. |
| `DesiredState` | always | string | The state the task targeted. |
| `Message` | always | string | Human-readable result message (may be empty). |
| `Commands` | when a subprocess ran | list | Resolved dokku command lines executed. |
| `Stdout` | when a subprocess ran | string | Captured stdout of the final command. |
| `Stderr` | when a subprocess ran | string | Captured stderr of the final command. |
| `ExitCode` | when a subprocess ran | int | Exit code of the final command. |
