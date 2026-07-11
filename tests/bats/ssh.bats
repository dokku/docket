#!/usr/bin/env bats

load test_helper

setup() {
  require_remote_dokku
  docket_build
  ssh_clean_app
}

teardown() {
  if [ -n "${DOCKET_TEST_REMOTE_HOST:-}" ]; then
    ssh_clean_app || true
  fi
}

# ssh_clean_app destroys the per-test app on the remote host if it
# exists. Mirrors dokku_clean_app but routes through ssh so the bats
# host does not need a local dokku binary.
ssh_clean_app() {
  local app="docket-test-ssh"
  if ssh -o BatchMode=yes "$DOCKET_TEST_REMOTE_HOST" "dokku apps:exists $app" >/dev/null 2>&1; then
    ssh -o BatchMode=yes "$DOCKET_TEST_REMOTE_HOST" "dokku --force apps:destroy $app" >/dev/null 2>&1 || true
  fi
}

@test "DOKKU_HOST routes apply through ssh" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-ssh
      dokku_app:
        app: docket-test-ssh
EOF
  DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run ssh -o BatchMode=yes "$DOCKET_TEST_REMOTE_HOST" "dokku apps:exists docket-test-ssh"
  assert_success
}

@test "play header annotates host when DOKKU_HOST is set" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-ssh
      dokku_app:
        app: docket-test-ssh
EOF
  DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "(host: $DOCKET_TEST_REMOTE_HOST)"
}

@test "DOKKU_HOST plan does not mutate remote state" {
  ssh_clean_app
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-ssh
      dokku_app:
        app: docket-test-ssh
EOF
  DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[+]"
  run ssh -o BatchMode=yes "$DOCKET_TEST_REMOTE_HOST" "dokku apps:exists docket-test-ssh"
  assert_failure
}

@test "ssh transport failure renders ssh-prefixed error during plan" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-ssh
      dokku_app:
        app: docket-test-ssh
EOF
  # Probes propagate *subprocess.SSHError, so plan surfaces transport
  # failures with the same `ssh:` prefix as apply does.
  DOKKU_HOST="$USER@127.0.0.1:1" run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "ssh:"
}

@test "--host flag overrides DOKKU_HOST env var" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-ssh
      dokku_app:
        app: docket-test-ssh
EOF
  DOKKU_HOST="bogus-should-not-be-used" run "$(docket_bin)" apply \
    --tasks "$TASKS_FILE" --host "$DOCKET_TEST_REMOTE_HOST"
  assert_success
  assert_output --partial "(host: $DOCKET_TEST_REMOTE_HOST)"
  refute_output --partial "(host: bogus-should-not-be-used)"
}

@test "ssh transmits argument values with spaces verbatim" {
  # A value with spaces exercises the transport quoting: OpenSSH space-joins
  # the remote argv and the remote login shell re-parses it, so without
  # quoting "npm run start" would reach dokku as three arguments.
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-ssh
      dokku_app:
        app: docket-test-ssh
    - name: set the start command
      dokku_ps_property:
        app: docket-test-ssh
        property: start-cmd
        value: npm run start
EOF
  DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success

  # The value round-tripped intact, so a re-plan reports no drift instead of
  # looping forever on a truncated "npm" (the "never converges" symptom).
  DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "(in sync)"
  refute_output --partial "[~]"

  # And the raw value stored on the server is exactly what we set.
  run ssh -o BatchMode=yes "$DOCKET_TEST_REMOTE_HOST" "dokku ps:report docket-test-ssh --format json"
  assert_success
  assert_output --partial "npm run start"
}
