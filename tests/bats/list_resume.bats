#!/usr/bin/env bats

load test_helper

# list_resume.bats covers the #212 inspection / resume surface end-to-end:
# --list-tasks on apply and plan renders the resolved task plan without
# executing or probing, --start-at-task on apply skips earlier tasks and
# runs from the matched task onward (including the inside-block case),
# and validate --strict surfaces unknown_play_reference /
# unknown_start_at_task problems for typo'd CLI references.

setup() {
  docket_build
  dokku_clean_app docket-test-list-1
  dokku_clean_app docket-test-list-2
  dokku_clean_app docket-test-list-tag-api
  dokku_clean_app docket-test-list-tag-worker
  dokku_clean_app docket-test-list-loop-a
  dokku_clean_app docket-test-list-loop-b
  dokku_clean_app docket-test-list-loop-c
  dokku_clean_app docket-test-start-at-first
  dokku_clean_app docket-test-start-at-second
  dokku_clean_app docket-test-start-at-third
  dokku_clean_app docket-test-start-at-unknown
  dokku_clean_app docket-test-start-block-a
  dokku_clean_app docket-test-start-block-b
  dokku_clean_app docket-test-start-block-c
}

teardown() {
  dokku_clean_app docket-test-list-1
  dokku_clean_app docket-test-list-2
  dokku_clean_app docket-test-list-tag-api
  dokku_clean_app docket-test-list-tag-worker
  dokku_clean_app docket-test-list-loop-a
  dokku_clean_app docket-test-list-loop-b
  dokku_clean_app docket-test-list-loop-c
  dokku_clean_app docket-test-start-at-first
  dokku_clean_app docket-test-start-at-second
  dokku_clean_app docket-test-start-at-third
  dokku_clean_app docket-test-start-at-unknown
  dokku_clean_app docket-test-start-block-a
  dokku_clean_app docket-test-start-block-b
  dokku_clean_app docket-test-start-block-c
}

@test "--list-tasks prints resolved plan without running" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app: { app: docket-test-list-1 }
    - dokku_app: { app: docket-test-list-2 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "docket-test-list-1"
  assert_output --partial "docket-test-list-2"
  run dokku apps:exists docket-test-list-1
  assert_failure # never created
}

@test "--list-tasks reports a null task body without panicking" {
  # apply --list-tasks is offline; a null task body used to panic the
  # loader (#306). It must now surface a clean parse error instead.
  write_tasks_file <<EOF
---
- tasks:
    - name: x
      dokku_app:
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial "body must not be empty"
  refute_output --partial "panic:"
}

@test "--list-tasks honors --tags filter" {
  write_tasks_file <<EOF
---
- tasks:
    - name: api task
      tags: [api]
      dokku_app: { app: docket-test-list-tag-api }
    - name: worker task
      tags: [worker]
      dokku_app: { app: docket-test-list-tag-worker }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --tags api
  assert_success
  assert_output --partial "api task"
  refute_output --partial "worker task"
}

@test "--list-tasks expands loops" {
  # An unnamed loop whose item feeds an identity field names each iteration
  # after the resource that iteration addresses (#427), which is both more
  # informative than an (item=) suffix and the form --start-at-task accepts.
  write_tasks_file <<EOF
---
- tasks:
    - loop: [a, b, c]
      dokku_app: { app: "docket-test-list-loop-{{ .item }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "dokku_app[app=docket-test-list-loop-a]"
  assert_output --partial "dokku_app[app=docket-test-list-loop-b]"
  assert_output --partial "dokku_app[app=docket-test-list-loop-c]"
}

@test "--list-tasks names a named loop by item" {
  # A loop the recipe author named keeps the <name> (item=<value>) form: the
  # author already said what to call it.
  write_tasks_file <<EOF
---
- tasks:
    - name: create apps
      loop: [a, b]
      dokku_app: { app: "docket-test-list-named-{{ .item }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "create apps (item=a)"
  assert_output --partial "create apps (item=b)"
}

@test "--list-tasks keeps both iterations for duplicate loop items" {
  # Duplicate scalar items used to collide into one envelope name and
  # trip the duplicate-name guard, dropping an iteration (#320). Both
  # iterations must survive with disambiguated names - here each renders a
  # different app, so each addresses a different resource.
  write_tasks_file <<EOF
---
- tasks:
    - loop: [web, web]
      dokku_app: { app: "docket-test-list-dup-{{ .index }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "dokku_app[app=docket-test-list-dup-0]"
  assert_output --partial "dokku_app[app=docket-test-list-dup-1]"
  refute_output --partial "duplicate task name"
}

@test "--list-tasks falls back to item names when a loop addresses one resource" {
  # Every iteration here sets a key on the same app, so every iteration
  # addresses the same resource. The fallback is all-or-nothing so one loop
  # never renders a mix of addresses and item suffixes.
  write_tasks_file <<EOF
---
- tasks:
    - loop: [A, B]
      dokku_config:
        app: docket-test-list-same
        config: { "{{ .item }}": "1" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "dokku_config (item=A)"
  assert_output --partial "dokku_config (item=B)"
  refute_output --partial "duplicate task name"
}

@test "--list-tasks names unnamed tasks after the resource they manage" {
  # The old display heuristic keyed on the first App-like field, so every
  # phase and option of one app collapsed onto the same label (#427).
  write_tasks_file <<EOF
---
- tasks:
    - dokku_docker_options:
        app: docket-test-list-opts
        phase: deploy
        option: "--memory=512m"
    - dokku_docker_options:
        app: docket-test-list-opts
        phase: build
        option: "--shm-size=1g"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "dokku_docker_options[app=docket-test-list-opts,phase=deploy,option=--memory=512m]"
  assert_output --partial "dokku_docker_options[app=docket-test-list-opts,phase=build,option=--shm-size=1g]"
  refute_output --partial "task #"
}

@test "--list-tasks output is identical across runs" {
  # The correlation guarantee: a consumer diffing one run against another has
  # to be able to line the tasks up. Before #427 unnamed tasks carried eight
  # random bytes and nothing lined up.
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app: { app: docket-test-list-stable }
    - dokku_config: { app: docket-test-list-stable, config: { A: "1" } }
    - dokku_config: { app: docket-test-list-stable, state: absent, config: { B: "" } }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  first="$output"
  assert_output --partial "dokku_config[app=docket-test-list-stable]"
  assert_output --partial "dokku_config[app=docket-test-list-stable] #2"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  [ "$output" = "$first" ]
}

@test "--start-at-task resumes at a generated name" {
  # An unnamed task could not be named on the command line at all before
  # #427, because its name differed on every run.
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app: { app: docket-test-resume-1 }
    - dokku_app: { app: docket-test-resume-2 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --start-at-task 'dokku_app[app=docket-test-resume-2]'
  assert_success
  assert_output --partial "dokku_app[app=docket-test-resume-2]"
}

@test "validate --strict accepts a generated --start-at-task name" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app: { app: docket-test-validate-resume }
    - dokku_config: { app: docket-test-validate-resume, config: { A: "1" } }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --start-at-task 'dokku_config[app=docket-test-validate-resume]'
  assert_success
  refute_output --partial "unknown_start_at_task"
}

@test "validate --strict still rejects an unknown --start-at-task name" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app: { app: docket-test-validate-typo }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --start-at-task 'dokku_app[app=nope]'
  assert_failure
  assert_output --partial 'does not match any task in the recipe'
  assert_output --partial "dokku_app[app=docket-test-validate-typo]"
}

@test "--list-tasks shows [skipped] for when:false" {
  write_tasks_file <<EOF
---
- tasks:
    - name: gated
      when: 'false'
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "[skipped] gated"
}

@test "--list-tasks shows [unknown] for a rescue when: on failed_task" {
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy
      block:
        - dokku_app: { app: docket-test-list-1 }
      rescue:
        - name: report
          when: 'failed_task.Stderr contains "already exists"'
          dokku_app: { app: docket-test-list-1, state: absent }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "[unknown] [rescue] report"
  refute_output --partial "[when?]"
}

@test "--list-tasks exits 1 on a task when: that cannot evaluate" {
  write_tasks_file <<EOF
---
- tasks:
    - name: broken predicate
      when: '[][0] == 1'
      dokku_app: { app: docket-test-list-1 }
    - name: plain
      dokku_app: { app: docket-test-list-2 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial "[when?]   broken predicate"
  assert_output --partial "plain"
}

@test "--list-tasks marks deprecated task types" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure storage
      dokku_storage_ensure:
        app: docket-test-list-1
        chown: herokuish
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "ensure storage"
  assert_output --partial "(deprecated)"
}

@test "--list-tasks --json sets deprecated:true on deprecated tasks" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure storage
      dokku_storage_ensure:
        app: docket-test-list-1
        chown: herokuish
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --json
  assert_success
  assert_output --partial '"deprecated":true'
  assert_output --partial '"deprecation":"'
}

@test "--list-tasks marks task types that never converge" {
  write_tasks_file <<EOF
---
- tasks:
    - name: registry auth
      dokku_registry_auth:
        global: true
        server: docker.io
        username: deploy-bot
        password: examplepassword
    - name: create app
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "registry auth  (never converges)"
  refute_output --partial "create app  (never converges)"
}

@test "--list-tasks marks task types with a partial probe" {
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy from image
      dokku_git_from_image:
        app: docket-test-list-1
        image: dokku/smoke-test-app:dockerfile
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "deploy from image  (partial probe)"
}

@test "--list-tasks --json sets probe on tasks that cannot read their state" {
  write_tasks_file <<EOF
---
- tasks:
    - name: registry auth
      dokku_registry_auth:
        global: true
        server: docker.io
        username: deploy-bot
        password: examplepassword
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --json
  assert_success
  assert_output --partial '"probe":"unsupported"'
  assert_output --partial '"probe_caveat":"'
}

@test "--list-tasks --json omits probe on fully probed tasks" {
  write_tasks_file <<EOF
---
- tasks:
    - name: create app
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --json
  assert_success
  refute_output --partial '"probe"'
}

@test "--list-tasks masks a sensitive input in a generated task name" {
  # #455: the listing renders resolved values, and since #427 an unnamed task
  # is named after the resource it addresses - so a sensitive input
  # interpolated into an identity field lands in the name.
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: app_name, required: true, sensitive: true }
  tasks:
    - dokku_app: { app: "{{ .app_name }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --app_name=listnamezzz --list-tasks
  assert_success
  refute_output --partial "listnamezzz"
  assert_output --partial "dokku_app[app=***]"
}

@test "--list-tasks --json masks a sensitive input in name and loop_item" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: deploy
      loop: 'split(secret_value, ",")'
      dokku_app: { app: "{{ .item }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=listloopzzz --list-tasks --json
  assert_success
  refute_output --partial "listloopzzz"
  assert_output --partial '"loop_item":"***"'
  assert_output --partial '"name":"deploy (item=***)"'
}

@test "--list-tasks masks a sensitive input resolved from its default" {
  # #490: the listing renders resolved values, and with no override the
  # resolved value is the input's own `default:`. It reaches the mask registry
  # through the same Argument.StringValue() a --vars-file or CLI value does, so
  # a secret written into `default:` masks like any other.
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: app_name, default: listdefaultzzz, sensitive: true }
  tasks:
    - dokku_app: { app: "{{ .app_name }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  refute_output --partial "listdefaultzzz"
  assert_output --partial "dokku_app[app=***]"
}

@test "--list-tasks masks a whitespace-padded sensitive loop item in the name" {
  # #473: the `(item=<value>)` suffix renders the item through TrimSpace, so a
  # secret carrying leading or trailing whitespace stopped matching the literal
  # registered in the mask registry, and printed in the clear.
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: deploy
      loop: 'split(secret_value, ",")'
      dokku_app: { app: "{{ .item }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=" listpaddedzzz " --list-tasks --json
  assert_success
  refute_output --partial "listpaddedzzz"
  assert_output --partial '"name":"deploy (item=***)"'
}

@test "--list-tasks masks a quote-bearing sensitive identity value in the name" {
  # #475: a generated address wraps a key value in Go quoting when a bare form
  # would not parse back, which escapes the quote the value carries, so the
  # secret stopped matching the literal registered in the mask registry and
  # printed in the clear inside its own address.
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_app: { app: "{{ .secret_value | dq }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value='listquotedzzz"x' --list-tasks --json
  assert_success
  refute_output --partial "listquotedzzz"
  assert_output --partial '"name":"dokku_app[app=\"***\"]"'
}

@test "--list-tasks masks a task-declared sensitive value" {
  # Nothing here is a sensitive *input*: dokku_config declares its whole
  # config: map sensitive, so the value reaches the mask registry only via
  # CollectPlaySensitiveValues, which #455 moved above the listing branch.
  write_tasks_file <<'EOF'
---
- tasks:
    - name: configure
      loop: [listconfigzzz]
      dokku_config:
        app: docket-test-list-1
        config:
          TOKEN: "{{ .item }}"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --json
  assert_success
  refute_output --partial "listconfigzzz"
  assert_output --partial '"loop_item":"***"'
}

@test "--list-tasks masks a sensitive input in a play name and when:" {
  write_tasks_file <<'EOF'
---
- name: play {{ .secret_value }}
  inputs:
    - { name: secret_value, required: true, sensitive: true }
  when: '"{{ .secret_value }}" == "will-not-match"'
  tasks:
    - dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=listplayzzz --list-tasks
  assert_success
  refute_output --partial "listplayzzz"
  assert_output --partial "==> Play: play ***"
  assert_output --partial "(skipped:"
}

@test "--list-tasks works on plan as well" {
  write_tasks_file <<EOF
---
- tasks:
    - name: probe
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "[0] probe"
}

@test "--start-at-task skips earlier tasks" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: first
      dokku_app: { app: docket-test-start-at-first }
    - name: second
      dokku_app: { app: docket-test-start-at-second }
    - name: third
      dokku_app: { app: docket-test-start-at-third }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --start-at-task second
  assert_success
  assert_output --partial "[skipped] first"
  assert_output --partial "before --start-at-task"
  run dokku apps:exists docket-test-start-at-first
  assert_failure # skipped
  run dokku apps:exists docket-test-start-at-second
  assert_success
  run dokku apps:exists docket-test-start-at-third
  assert_success
}

@test "--start-at-task with unknown name errors" {
  write_tasks_file <<EOF
---
- tasks:
    - name: first
      dokku_app: { app: docket-test-start-at-unknown }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --start-at-task no-such-task
  assert_failure
  assert_output --partial "no task matched name"
  assert_output --partial '"first"'
}

@test "--start-at-task hint masks a quote-bearing sensitive identity value" {
  # #475: the hint renders every available name through Go quoting, a second
  # escaping layer on top of the one a generated address already carries, so
  # the name is masked before it is quoted rather than after.
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_app: { app: "{{ .secret_value | dq }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value='hintquotedzzz"x' --start-at-task no-such-task
  assert_failure
  refute_output --partial "hintquotedzzz"
  assert_output --partial 'dokku_app[app=\"***\"]'
}

@test "--start-at-task matching a block child runs from that child" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy-group
      block:
        - name: block-a
          dokku_app: { app: docket-test-start-block-a }
        - name: block-b
          dokku_app: { app: docket-test-start-block-b }
        - name: block-c
          dokku_app: { app: docket-test-start-block-c }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --start-at-task block-b
  assert_success
  assert_output --partial "[skipped] [block] block-a"
  assert_output --partial "before --start-at-task"
  run dokku apps:exists docket-test-start-block-a
  assert_failure
  run dokku apps:exists docket-test-start-block-b
  assert_success
  run dokku apps:exists docket-test-start-block-c
  assert_success
}

@test "validate --strict --play missing reports unknown_play_reference" {
  write_tasks_file <<EOF
---
- name: api
  tasks:
    - dokku_app: { app: docket-test-list-1 }
- name: worker
  tasks:
    - dokku_app: { app: docket-test-list-2 }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --play missing
  assert_failure
  assert_output --partial '--play "missing"'
  assert_output --partial '"api"'
  assert_output --partial '"worker"'
}

@test "validate --strict --start-at-task missing reports unknown_start_at_task" {
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --start-at-task missing-task
  assert_failure
  assert_output --partial '--start-at-task "missing-task"'
  assert_output --partial '"deploy"'
}
