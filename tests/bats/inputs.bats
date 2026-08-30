#!/usr/bin/env bats

load test_helper

# inputs.bats covers what a recipe's `inputs:` block declares - the type, the
# default, and whether a value has to be supplied. Nothing here contacts a
# server: FlagSet() registers a flag per input before Run, `--list-tasks`
# renders the resolved plan and returns, and `validate` is offline by contract.
#
# The regression is #493. A `type: bool` input that declared no `default:` made
# registerInputFlags error, and every FlagSet() caller discarded that error, so
# the flag set came back carrying only the built-in flags. One input written the
# obvious way cost *every* input its flag, with no message at all.

setup() {
  docket_build
}

@test "a bool input with no default does not cost the other inputs their flags" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: debug, type: bool }
    - { name: app_name, default: web }
  tasks:
    - dokku_app: { app: "{{ .app_name }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --app_name=override
  assert_success
  assert_output --partial "dokku_app[app=override]"
}

@test "apply --help lists every input alongside a bool with no default" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: debug, type: bool }
    - { name: app_name, default: web }
  tasks:
    - dokku_app: { app: "{{ .app_name }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --help
  assert_success
  assert_output --partial "--debug"
  assert_output --partial "--app_name"
}

@test "an omitted default is the zero value for the type" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: debug, type: bool }
    - { name: replicas, type: int }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}-{{ .replicas }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "dokku_app[app=web-false-0]"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --debug=true --replicas=3
  assert_success
  assert_output --partial "dokku_app[app=web-true-3]"
}

@test "a JSON5 recipe registers inputs whose default is not a string" {
  write_tasks_file tasks.json <<'EOF'
[
  { inputs: [{ name: "port", type: "int", default: 8080 },
             { name: "debug", type: "bool" }],
    tasks: [{ dokku_app: { app: "web-{{ .debug }}-{{ .port }}" } }] },
]
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "dokku_app[app=web-false-8080]"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --debug=true --port=9000
  assert_success
  assert_output --partial "dokku_app[app=web-true-9000]"
}

@test "docket validate reports an unknown input type" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: port, type: intt, default: "8080" }
  tasks:
    - dokku_app: { app: web }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial 'input "port" declares unknown type "intt"'
  assert_output --partial 'did you mean "int"?'

  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"invalid_input_type"'
}

@test "docket validate reports a default that does not parse as its type" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: port, type: int, default: abc }
  tasks:
    - dokku_app: { app: web }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial 'its default "abc" is not a valid int'

  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"invalid_input_default"'
}

@test "docket validate accepts an omitted default on every type" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: debug, type: bool }
    - { name: replicas, type: int }
    - { name: ratio, type: float }
    - { name: app_name, type: string }
  tasks:
    - dokku_app: { app: web }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
}

@test "apply --list-tasks rejects a malformed input declaration offline" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: port, type: int, default: abc }
  tasks:
    - dokku_app: { app: web }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial 'its default "abc" is not a valid int'
  refute_output --partial "panic:"
  refute_output --partial "unknown flag"
}

@test "a required input with no default stops the run on a non-string type" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: debug, type: bool, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial "Missing flag '--debug'"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --debug=false
  assert_success
  assert_output --partial "dokku_app[app=web-false]"
}

@test "an explicitly empty value does not satisfy a required input" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: app, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .app }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --app=
  assert_failure
  assert_output --partial "Missing flag '--app'"
  refute_output --partial "<no value>"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --app=api
  assert_success
  assert_output --partial "dokku_app[app=web-api]"
}

@test "docket validate --strict flags a required bool with no default" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: debug, type: bool, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict
  assert_failure
  assert_output --partial 'input "debug" is required'

  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --debug=false
  assert_success
}
