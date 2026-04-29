#!/usr/bin/env bats

load test_helper

# block.bats covers the #211 try/catch/finally surface end-to-end
# against the docket binary: block runs all children when none error,
# rescue handles the first error, always runs unconditionally,
# ignore_errors on a child does not trigger rescue, and loop on a
# block expands the entire group N times.
#
# Tests that mutate live Dokku state gate on require_dokku.

setup() {
  docket_build
}

teardown() {
  dokku_clean_app docket-test-block-1
  dokku_clean_app docket-test-block-2
  dokku_clean_app docket-test-block-rescue
  dokku_clean_app docket-test-block-rescued
  dokku_clean_app docket-test-block-rescued-zzz
  dokku_clean_app docket-test-block-always-1
  dokku_clean_app docket-test-block-always-marker
  dokku_clean_app docket-test-block-swallowed-after
  dokku_clean_app docket-test-loop-block-api
  dokku_clean_app docket-test-loop-block-worker
  dokku_clean_app docket-test-loop-block-web
}

@test "block runs all children when none error" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy
      block:
        - dokku_app: { app: docket-test-block-1 }
        - dokku_app: { app: docket-test-block-2 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-block-1
  assert_success
  run dokku apps:exists docket-test-block-2
  assert_success
}

@test "rescue runs on block error" {
  require_dokku
  dokku apps:create docket-test-block-rescue || true
  write_tasks_file <<EOF
---
- tasks:
    - name: try
      block:
        - dokku_ports:
            app: nonexistent-block-target-zzz
            port_mappings: [{ scheme: http, host_port: 80, container_port: 5000 }]
            state: present
      rescue:
        - dokku_app: { app: docket-test-block-rescue, state: absent }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-block-rescue
  assert_failure
}

@test "always runs after success" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: with-always
      block:
        - dokku_app: { app: docket-test-block-always-1 }
      always:
        - dokku_app: { app: docket-test-block-always-marker }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-block-always-marker
  assert_success
}

@test "ignore_errors on a block child does not trigger rescue" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: swallow
      block:
        - name: errors quietly
          ignore_errors: true
          dokku_ports:
            app: nonexistent-block-swallow-zzz
            port_mappings: [{ scheme: http, host_port: 80, container_port: 5000 }]
            state: present
        - dokku_app: { app: docket-test-block-swallowed-after }
      rescue:
        - dokku_app: { app: docket-test-block-rescued-zzz }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-block-swallowed-after
  assert_success
  run dokku apps:exists docket-test-block-rescued-zzz
  assert_failure
}

@test "loop on a block runs entire group N times" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy each
      loop: [api, worker, web]
      block:
        - dokku_app: { app: "docket-test-loop-block-{{ .item }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  for app in api worker web; do
    run dokku apps:exists "docket-test-loop-block-$app"
    assert_success
  done
}

@test "validate flags empty block" {
  write_tasks_file <<EOF
---
- tasks:
    - name: empty
      block: []
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "block: must contain at least one child task"
}

@test "validate flags rescue without block" {
  write_tasks_file <<EOF
---
- tasks:
    - name: orphan
      rescue:
        - dokku_app: { app: x }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "rescue: requires a block: in the same task entry"
}
