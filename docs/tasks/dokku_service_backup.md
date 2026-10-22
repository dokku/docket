# dokku_service_backup

## Synopsis

Manages the S3 backup schedule, authentication, and encryption for a dokku service

## Requirements

- Dokku >= 0.38.31
- a dokku datastore service plugin >= 2.3.1 matching the service type (dokku-datastore 0.7.1, e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Partial - the backup schedule, bucket, and use_iam flag are exported; the AWS credentials and encryption passphrase are reported only as fingerprints, and the region, signature version and endpoint cannot be declared without the credentials, so all of these must be re-supplied before the recipe can back up.

## Probe support

Supported.

## Lifecycle

Applied with state `present` and torn down with state `absent`. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `service` and `name`. Fields left empty are omitted from the address.

## Objects

- The datastore service named by `service` and `name`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to back up (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `schedule` | string | no |  |  | Cron schedule for backups |
| `bucket` | string | no |  |  | S3 bucket backups are written to |
| `use_iam` | bool | no |  |  | Use the instance IAM profile instead of static credentials |
| `aws_access_key_id` | string | no |  |  | AWS access key id for backup auth (not returned when reading state) |
| `aws_secret_access_key` | string | no |  |  | AWS secret access key for backup auth (not returned when reading state) (sensitive) |
| `aws_default_region` | string | no |  |  | AWS region used for backup auth |
| `aws_signature_version` | string | no |  |  | AWS signature version used for backup auth |
| `endpoint_url` | string | no |  |  | S3-compatible endpoint url used for backup auth |
| `encryption_passphrase` | string | no |  |  | Passphrase used to encrypt future backups (not returned when reading state) (sensitive) |
| `state` | string | no | present | present, absent | Desired state of the backup configuration. 'absent' removes whichever of the schedule, the auth settings and the encryption passphrase the service reports as stored, without needing the secrets in the recipe, and leaves the settings dokku_service_property manages alone. On a datastore that cannot be backed up, 'absent' is always in sync and 'present' fails with the plugin's own error. |

## Constraints

- `schedule` and `bucket` must be set together or not at all when `state` is `present`.
- `aws_access_key_id` and `aws_secret_access_key` must be set together or not at all when `state` is `present`.
- `aws_default_region` requires `aws_access_key_id` and `aws_secret_access_key` when `state` is `present`.
- `aws_signature_version` requires `aws_access_key_id`, `aws_secret_access_key` and `aws_default_region` when `state` is `present`.
- `endpoint_url` requires `aws_access_key_id`, `aws_secret_access_key` and `aws_signature_version` when `state` is `present`.
- At least one of `schedule`, `aws_access_key_id`, `aws_secret_access_key` or `encryption_passphrase` must be set when `state` is `present`.

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
