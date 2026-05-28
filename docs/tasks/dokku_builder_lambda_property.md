# dokku_builder_lambda_property

Manages the builder-lambda configuration for a given dokku application

## Setting the lambda.yml path for an app

```yaml
dokku_builder_lambda_property:
    app: node-js-app
    property: lambdayml-path
    value: config/lambda.yml
```

## Setting the lambda.yml path globally

```yaml
dokku_builder_lambda_property:
    app: ""
    global: true
    property: lambdayml-path
    value: lambda.yml
```

## Clearing the lambda.yml path for an app

```yaml
dokku_builder_lambda_property:
    app: node-js-app
    property: lambdayml-path
```
