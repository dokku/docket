#!/usr/bin/env bats

load test_helper

# plays.bats covers the #208 multi-play surface end-to-end against the
# docket binary: a tasks.yml with multiple top-level plays runs every
# play in order, --play <name> filters to one play, --fail-fast reverts
# to the abort-entire-run semantics, and a play-level when: predicate
# skips the entire play. Variable-visibility scoping for play-level
# when: is exercised against the plan path so tests do not require a
# Dokku server.

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

# Variable-visibility tests for play-level when: drive the plan path so
# they do not require a Dokku server. The pattern is:
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
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
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
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
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
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --env prod
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
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.yml"
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
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
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
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "==> Play: api"
  assert_output --partial '==> Play: worker  (skipped'
}

@test "plays: per-play inputs scope to their own play in task body" {
  write_tasks_file <<EOF
---
- name: api
  inputs:
    - name: app
      default: docket-test-noop-api
  tasks:
    - name: noop
      dokku_app:
        app: "{{ .app }}"
- name: worker
  inputs:
    - name: app
      default: docket-test-noop-worker
  tasks:
    - name: noop
      dokku_app:
        app: "{{ .app }}"
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "docket-test-noop-api"
  assert_output --partial "docket-test-noop-worker"
}
