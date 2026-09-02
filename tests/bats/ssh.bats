#!/usr/bin/env bats

load test_helper

# SSH_GIT_AUTH_HOST is a hostname docket never contacts; it only ever names a
# netrc entry on the dokku server.
SSH_GIT_AUTH_HOST="docket-test-ssh.example.com"

setup() {
  require_remote_dokku
  docket_build
  ssh_clean_app
  ssh_clean_git_auth
}

teardown() {
  if [ -n "${DOCKET_TEST_REMOTE_HOST:-}" ]; then
    ssh_clean_app || true
    ssh_clean_git_auth || true
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

# ssh_clean_git_auth drops the per-test netrc entry on the remote host.
# git:auth with no username clears the host, and is a no-op when there is
# nothing to clear.
ssh_clean_git_auth() {
  ssh -o BatchMode=yes "$DOCKET_TEST_REMOTE_HOST" \
    "dokku --quiet git:auth $SSH_GIT_AUTH_HOST" >/dev/null 2>&1 || true
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

@test "ssh transport failure on a toggle task surfaces as a plan error" {
  write_tasks_file <<EOF
---
- tasks:
    - name: enable the proxy
      dokku_proxy_toggle:
        app: docket-test-ssh
EOF
  # A toggle task's probe also propagates *subprocess.SSHError, so an
  # unreachable host is a plan error (exit 1) rather than spurious drift
  # that would exit 0.
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

# The netrc password reaches dokku on stdin rather than on argv, and dokku only
# reads it when stdin is a pipe. Locally that is guaranteed by os/exec; over ssh
# it depends on how sshd wires the remote command's stdin, which nothing else in
# the suite exercises. A converging re-plan is the proof: the probe could only
# have matched if git:auth-status received the same password git:auth stored.
@test "dokku_git_auth converges over ssh with the password on stdin" {
  write_tasks_file <<EOF
---
- tasks:
    - name: configure git auth
      dokku_git_auth:
        host: $SSH_GIT_AUTH_HOST
        username: deploy-bot
        password: ghp_examplepat
EOF
  DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[changed]"

  DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "0 would change"
  refute_output --partial "[~]"

  # The password is masked in the run's own output, but it must not be on the
  # remote argv either - that is what stdin buys.
  DOKKU_TRACE=1 DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  refute_output --partial "ghp_examplepat"
}

# argv_recipe is the one-task recipe the transport-argv cases below plan
# against. They use `plan` rather than `apply` deliberately: the assertion is
# about the ssh argv docket builds, and probing reads the server without
# leaving root-owned state behind for the next test to trip over.
argv_recipe() {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-ssh
      dokku_app:
        app: docket-test-ssh
EOF
}

@test "--sudo wraps the remote dokku call in sudo -n" {
  argv_recipe
  # DOKKU_TRACE echoes the ssh argv, which is the only place the remote sudo
  # wrap is visible: it happens on the server, so the command docket reports is
  # the bare `dokku ...` form either way.
  DOKKU_TRACE=1 DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan \
    --tasks "$TASKS_FILE" --sudo
  assert_success
  assert_output --partial "-- sudo -n dokku"
}

@test "DOKKU_SUDO=1 is equivalent to --sudo over ssh" {
  argv_recipe
  # The argv builder used to read DOKKU_SUDO out of the process environment
  # itself. It now takes the setting from the target the commands layer
  # resolved, so this is what proves the env var still reaches it.
  DOKKU_TRACE=1 DOKKU_SUDO=1 DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan \
    --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "-- sudo -n dokku"
}

@test "no sudo wrap without --sudo or DOKKU_SUDO" {
  argv_recipe
  DOKKU_TRACE=1 DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan \
    --tasks "$TASKS_FILE"
  assert_success
  refute_output --partial "sudo -n"
}

@test "--accept-new-host-keys adds the ssh option" {
  argv_recipe
  DOKKU_TRACE=1 DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan \
    --tasks "$TASKS_FILE" --accept-new-host-keys
  assert_success
  assert_output --partial "StrictHostKeyChecking=accept-new"
}

@test "DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1 is equivalent to the flag" {
  argv_recipe
  DOKKU_TRACE=1 DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1 DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" \
    run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "StrictHostKeyChecking=accept-new"
}

@test "the host-key option is absent unless asked for" {
  argv_recipe
  DOKKU_TRACE=1 DOKKU_HOST="$DOCKET_TEST_REMOTE_HOST" run "$(docket_bin)" plan \
    --tasks "$TASKS_FILE"
  assert_success
  refute_output --partial "StrictHostKeyChecking=accept-new"
}
