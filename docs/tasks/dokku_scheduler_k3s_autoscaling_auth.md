# dokku_scheduler_k3s_autoscaling_auth

## Synopsis

Manages KEDA TriggerAuthentication metadata grouped under a single trigger for a dokku application or globally

## Export support

Not supported - scheduler-k3s exposes no report for KEDA trigger authentication, so the current state cannot be read back (dokku/dokku#8800).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the trigger authentication should be applied globally |
| `trigger` | string | yes |  |  | Name of the KEDA trigger authentication resource |
| `metadata` | dict | no |  |  | Map of metadata key to value for the trigger authentication. On absent, only the keys are read. |
| `state` | string | no | present | present, absent | Desired state of the trigger authentication metadata |

## Examples

### Set AWS secret manager trigger metadata on an app

```yaml
dokku_scheduler_k3s_autoscaling_auth:
    app: node-js-app
    trigger: aws-secret-manager
    metadata:
        awsRegion: us-east-1
        secretName: my-secret
```

### Set a global trigger authentication shared across apps

```yaml
dokku_scheduler_k3s_autoscaling_auth:
    app: ""
    global: true
    trigger: aws-secret-manager
    metadata:
        awsRegion: us-east-1
        roleArn: arn:aws:iam::123456789012:role/keda
```

### Clear specific metadata keys from an app's trigger

```yaml
dokku_scheduler_k3s_autoscaling_auth:
    app: node-js-app
    trigger: aws-secret-manager
    metadata:
        secretName: ""
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
