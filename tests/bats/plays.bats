#!/usr/bin/env bats

load test_helper

# plays.bats covers the #208 multi-play surface end-to-end against the
# docket binary: a tasks.yml with multiple top-level plays runs every
# play in order, --play <name> filters to one play, --fail-fast reverts
# to the abort-entire-run semantics, and a play-level when: predicate
# skips the entire play. Variable-visibility scoping for play-level
# when: is exercised through plan --list-tasks, which resolves play
# selection and returns before any server contact, so those tests need
# no Dokku server.

setup() {
  docket_build
  dokku_clean_app docket-test-play-first
  dokku_clean_app docket-test-play-second
  dokku_clean_app docket-test-playfilter-first
  dokku_clean_app docket-test-playfilter-second
  dokku_clean_app docket-test-play-skipped
  dokku_clean_app docket-test-play-kept
  dokku_clean_app docket-test-play-bail-ok
  dokku_clean_app docket-test-play-failfast
  dokku_clean_app docket-test-playtags-deploy-api
  dokku_clean_app docket-test-playtags-configure-api
  dokku_clean_app docket-test-playtags-deploy-worker
  dokku_clean_app docket-test-playlvltags-api
  dokku_clean_app docket-test-playlvltags-worker
}

teardown() {
  dokku_clean_app docket-test-play-first
  dokku_clean_app docket-test-play-second
  dokku_clean_app docket-test-playfilter-first
  dokku_clean_app docket-test-playfilter-second
  dokku_clean_app docket-test-play-skipped
  dokku_clean_app docket-test-play-kept
  dokku_clean_app docket-test-play-bail-ok
  dokku_clean_app docket-test-play-failfast
  dokku_clean_app docket-test-playtags-deploy-api
  dokku_clean_app docket-test-playtags-configure-api
  dokku_clean_app docket-test-playtags-deploy-worker
  dokku_clean_app docket-test-playlvltags-api
  dokku_clean_app docket-test-playlvltags-worker
}

@test "plays: multi-play tasks.yml runs all plays in order" {
  require_dokku
  write_tasks_file <<EOF
---
- name: first
  tasks:
    - dokku_app:
        app: docket-test-play-first
- name: second
  tasks:
    - dokku_app:
        app: docket-test-play-second
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "==> Play: first"
  assert_output --partial "==> Play: second"
  run dokku apps:exists docket-test-play-first
  assert_success
  run dokku apps:exists docket-test-play-second
  assert_success
}

@test "plays: --play filter runs only the named play" {
  require_dokku
  write_tasks_file <<EOF
---
- name: first
  tasks:
    - dokku_app:
        app: docket-test-playfilter-first
- name: second
  tasks:
    - dokku_app:
        app: docket-test-playfilter-second
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --play first
  assert_success
  assert_output --partial "==> Play: first"
  refute_output --partial "==> Play: second"
  run dokku apps:exists docket-test-playfilter-first
  assert_success
  run dokku apps:exists docket-test-playfilter-second
  assert_failure
}

@test "plays: --play with unknown name reports available plays" {
  write_tasks_file <<EOF
---
- name: first
  tasks:
    - dokku_app:
        app: docket-test-playfilter-first
- name: second
  tasks:
    - dokku_app:
        app: docket-test-playfilter-second
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --play missing
  assert_failure
  assert_output --partial '--play "missing"'
  assert_output --partial '"first"'
  assert_output --partial '"second"'
}

@test "plays: --play hint masks a sensitive input in a play name" {
  # #477: the hint listed every play's name in the clear, so a name that
  # interpolated a sensitive input leaked. Nothing here contacts the server -
  # the filter runs and returns before any dokku command.
  write_tasks_file <<'EOF'
---
- name: "play-{{ .secret_value }}"
  inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_app:
        app: docket-test-playfilter-first
- name: keepzzz
  tasks:
    - dokku_app:
        app: docket-test-playfilter-second
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=playleakzzz --play missing
  assert_failure
  refute_output --partial "playleakzzz"
  assert_output --partial '"play-***"'
  # A play name carrying no secret still prints in full.
  assert_output --partial '"keepzzz"'
}

@test "plays: plan --play hint masks a sensitive input in a play name" {
  write_tasks_file <<'EOF'
---
- name: "play-{{ .secret_value }}"
  inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_app:
        app: docket-test-playfilter-first
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --secret_value=playleakzzz --play missing
  assert_failure
  refute_output --partial "playleakzzz"
  assert_output --partial '"play-***"'
}

@test "plays: --play hint masks a task-declared secret in a play name" {
  # The task-declared values join the mask registry only after the --play
  # filter narrows the play list, so the hint registers them from the whole
  # file before it renders (#477).
  write_tasks_file <<'EOF'
---
- name: play-taskdeclaredzzz
  tasks:
    - name: configure the app
      dokku_config:
        app: docket-test-playfilter-first
        config:
          TOKEN: taskdeclaredzzz
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --play missing
  assert_failure
  refute_output --partial "taskdeclaredzzz"
  assert_output --partial '"play-***"'
}

@test "plays: --play composes with --tags to filter tasks within the play" {
  require_dokku
  write_tasks_file <<EOF
---
- name: api
  tasks:
    - name: deploy-api
      tags: [deploy]
      dokku_app:
        app: docket-test-playtags-deploy-api
    - name: configure-api
      tags: [configure]
      dokku_app:
        app: docket-test-playtags-configure-api
- name: worker
  tasks:
    - name: deploy-worker
      tags: [deploy]
      dokku_app:
        app: docket-test-playtags-deploy-worker
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --play api --tags deploy
  assert_success
  assert_output --partial "deploy-api"
  refute_output --partial "configure-api"
  refute_output --partial "deploy-worker"
}

@test "plays: play-level tags propagate to tasks for the --tags filter" {
  require_dokku
  write_tasks_file <<EOF
---
- name: api
  tags: [api]
  tasks:
    - name: deploy-api
      dokku_app:
        app: docket-test-playlvltags-api
- name: worker
  tags: [worker]
  tasks:
    - name: deploy-worker
      dokku_app:
        app: docket-test-playlvltags-worker
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --tags api
  assert_success
  assert_output --partial "deploy-api"
  refute_output --partial "deploy-worker"
}

@test "plays: play with when:false is skipped" {
  require_dokku
  write_tasks_file <<EOF
---
- name: skipped-play
  when: 'false'
  tasks:
    - dokku_app:
        app: docket-test-play-skipped
- name: kept-play
  tasks:
    - dokku_app:
        app: docket-test-play-kept
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "(skipped:"
  assert_output --partial "1 play skipped"
  run dokku apps:exists docket-test-play-skipped
  assert_failure
  run dokku apps:exists docket-test-play-kept
  assert_success
}

@test "plays: task error in play 1 does not abort play 2" {
  require_dokku
  write_tasks_file <<EOF
---
- name: failing
  tasks:
    - dokku_ports:
        app: nonexistent-app-xyz
        port_mappings:
          - { scheme: http, host: 80, container: 5000 }
        state: present
- name: ok
  tasks:
    - dokku_app:
        app: docket-test-play-bail-ok
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_failure
  run dokku apps:exists docket-test-play-bail-ok
  assert_success
}

@test "plays: --fail-fast aborts entire run on first error" {
  require_dokku
  write_tasks_file <<EOF
---
- name: failing
  tasks:
    - dokku_ports:
        app: nonexistent-app-xyz
        port_mappings:
          - { scheme: http, host: 80, container: 5000 }
        state: present
- name: would-run
  tasks:
    - dokku_app:
        app: docket-test-play-failfast
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --fail-fast
  assert_failure
  run dokku apps:exists docket-test-play-failfast
  assert_failure
}

@test "plays: invalid play-level when: reported by validate" {
  write_tasks_file <<EOF
---
- name: bad
  when: 'this is not valid expr ('
  tasks:
    - dokku_app:
        app: docket-test-noop
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "play when expression compile error"
}

@test "plays: play-level when: that errors fails --list-tasks" {
  # The compile-error case above is caught by validate. This is the other
  # half: a predicate that compiles and then errors at evaluation. A play
  # when: is resolved against the same context plan uses, so --list-tasks
  # reaches the same verdict plan does and exits non-zero rather than
  # reporting a listing a run could never produce. The second play still
  # lists - the failure is carried by the exit code, not by stopping the
  # walk. Offline: --list-tasks never contacts a server.
  write_tasks_file <<EOF
---
- name: broken
  when: '[][0] == 1'
  tasks:
    - dokku_app:
        app: docket-test-noop
- name: fine
  tasks:
    - name: listed
      dokku_app:
        app: docket-test-noop-2
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial "==> Play: broken  (when error:"
  assert_output --partial "[0] listed"
}

@test "plays: unknown play-level key reported by validate" {
  write_tasks_file <<EOF
---
- name: bad
  invalidkey: foo
  tasks:
    - dokku_app:
        app: docket-test-noop
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial 'unexpected play key "invalidkey"'
}

# Variable-visibility tests for play-level when: drive plan --list-tasks
# so they do not require a Dokku server. Plain `plan` probes the server
# for every play that actually runs, so a test asserting a play ran would
# fail without dokku rather than skip (#412). --list-tasks resolves the
# play when: against the same merged context and prints the same
# `==> Play:` header and `(skipped: when ...)` marker, then returns
# before any task is planned. The pattern is:
#  - file-level input default is visible to play when: (truthy makes the
#    play run; falsy makes it skip).
#  - CLI / vars-file overrides win.
#  - play-local input defaults are NOT visible to a play's own when:
#    nor to other plays' when:.

@test "plays: play when: sees file-level input default (truthy)" {
  write_tasks_file <<EOF
---
- inputs:
    - name: env
      default: prod
- name: api
  when: 'env == "prod"'
  tasks:
    - dokku_app:
        app: docket-test-noop
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "==> Play: api"
  refute_output --partial "==> Play: api  (skipped"
}

@test "plays: play when: sees file-level input default (falsy)" {
  write_tasks_file <<EOF
---
- inputs:
    - name: env
      default: staging
- name: api
  when: 'env == "prod"'
  tasks:
    - dokku_app:
        app: docket-test-noop
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial '==> Play: api  (skipped: when "env == \"prod\"")'
}

@test "plays: CLI input override wins for play when:" {
  write_tasks_file <<EOF
---
- inputs:
    - name: env
      default: staging
- name: api
  when: 'env == "prod"'
  tasks:
    - dokku_app:
        app: docket-test-noop
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks --env prod
  assert_success
  assert_output --partial "==> Play: api"
  refute_output --partial "==> Play: api  (skipped"
}

@test "plays: --vars-file value flows into play when:" {
  write_tasks_file <<EOF
---
- inputs:
    - name: env
      default: staging
- name: api
  when: 'env == "prod"'
  tasks:
    - dokku_app:
        app: docket-test-noop
EOF
  cat >"$BATS_TEST_TMPDIR/vars.yml" <<EOF
env: prod
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks --vars-file "$BATS_TEST_TMPDIR/vars.yml"
  assert_success
  assert_output --partial "==> Play: api"
  refute_output --partial "==> Play: api  (skipped"
}

@test "plays: play own input is NOT visible to its own when:" {
  write_tasks_file <<EOF
---
- name: api
  inputs:
    - name: enabled
      default: "true"
  when: 'enabled == "true"'
  tasks:
    - dokku_app:
        app: docket-test-noop
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial '(skipped:'
}

@test "plays: sibling play input is NOT visible to other play's when:" {
  write_tasks_file <<EOF
---
- name: api
  inputs:
    - name: app
      default: api
  tasks:
    - name: api-noop
      dokku_app:
        app: docket-test-noop-api
- name: worker
  when: 'app == "api"'
  tasks:
    - name: worker-noop
      dokku_app:
        app: docket-test-noop-worker
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "==> Play: api"
  assert_output --partial '==> Play: worker  (skipped'
}

# The tasks here are deliberately unnamed: --list-tasks prints a
# user-supplied name: verbatim, so a named task would render as `noop`
# in both plays and hide the value under test. Unnamed, the listing
# falls back to `<type>: <body identifier>` and shows the app each play
# rendered from its own input.
@test "plays: per-play inputs scope to their own play in task body" {
  write_tasks_file <<EOF
---
- name: api
  inputs:
    - name: app
      default: docket-test-noop-api
  tasks:
    - dokku_app:
        app: "{{ .app }}"
- name: worker
  inputs:
    - name: app
      default: docket-test-noop-worker
  tasks:
    - dokku_app:
        app: "{{ .app }}"
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "docket-test-noop-api"
  assert_output --partial "docket-test-noop-worker"
}

# #495: a play-local input default layered into the render context as the raw
# text the recipe wrote, while the same input reached the base context as the
# typed value flag registration produced. A play-local `type: bool, default: on`
# rendered as `on` where a file-level one rendered as `true`.
@test "plays: a play-local typed default resolves to its type" {
  write_tasks_file <<EOF
---
- inputs:
    - { name: file_debug, type: bool, default: on }
- name: api
  inputs:
    - { name: play_debug, type: bool, default: on }
    - { name: replicas, type: int, default: 007 }
  tasks:
    - dokku_app:
        app: "web-{{ .file_debug }}-{{ .play_debug }}-{{ .replicas }}"
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "web-true-true-7"
}

# #497: an input reached a predicate as the pointer pflag allocated for its
# flag, and expr only dereferences the operands of an operator - a predicate
# that is nothing but an identifier got the pointer, which is never nil for a
# bool, and was true whatever the input held.
@test "plays: a bare when: reads a play-local bool input" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: file_debug, type: bool, default: false }
- name: api
  inputs:
    - { name: play_debug, type: bool, default: false }
  tasks:
    - name: from-file-level
      when: file_debug
      dokku_app: { app: docket-test-noop-a }
    - name: from-play-local
      when: play_debug
      dokku_app: { app: docket-test-noop-b }
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "[skipped] from-file-level"
  assert_output --partial "[skipped] from-play-local"

  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks --file_debug=true --play_debug=true
  assert_success
  refute_output --partial "[skipped]"
}
