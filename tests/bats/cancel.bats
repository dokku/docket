#!/usr/bin/env bats

load test_helper

# HANG_HOST is an RFC 5737 documentation address, guaranteed never to be routed
# to a real machine. An `ssh` to it blocks in the TCP handshake, which is the
# shape of hang docket cannot detect on its own and the operator ends by
# pressing Ctrl-C.
HANG_HOST="192.0.2.1"

# require_hanging_host skips unless connecting to HANG_HOST really does hang.
# On a network that answers "no route to host" the ssh child exits at once,
# apply fails on its own, and there is nothing left for an interrupt to
# demonstrate.
require_hanging_host() {
  if [ "$(uname -s)" != "Linux" ]; then
    skip "interrupt tests run on Linux only"
  fi
  if ! command -v timeout >/dev/null 2>&1; then
    skip "timeout(1) not available"
  fi
  if ! command -v ssh >/dev/null 2>&1; then
    skip "ssh client not available"
  fi
  local rc=0
  timeout 3 ssh -o BatchMode=yes -o StrictHostKeyChecking=no "$HANG_HOST" true >/dev/null 2>&1 || rc=$?
  # 124 is timeout(1) reporting it had to stop a process that was still running.
  if [ "$rc" -ne 124 ]; then
    skip "connecting to $HANG_HOST does not hang on this network"
  fi
}

setup() {
  require_hanging_host
  docket_build
  # Two plays, not two tasks in one: an error already ends the play it happened
  # in, so a second task in the same play would prove nothing. A second *play*
  # is what a run that only cancelled the task in flight would go on to start -
  # hanging on its own SSH handshake until timeout escalated to SIGKILL.
  write_tasks_file <<EOF
---
- name: one
  tasks:
    - name: first
      dokku_app:
        app: docket-test-cancel-one
- name: two
  tasks:
    - name: second
      dokku_app:
        app: docket-test-cancel-two
EOF
}

@test "SIGINT aborts the whole apply, not just the task in flight" {
  # The first task blocks in the SSH handshake. SIGINT arrives two seconds in;
  # SIGKILL follows eight seconds after that if docket ignored it. Cancelling
  # the run context ends the run, so docket exits on the interrupt and the
  # second play never opens a connection of its own - which used to take one
  # Ctrl-C per remaining play.
  #
  # --preserve-status is what makes the status meaningful: without it timeout
  # reports 124 whenever its timer fired, however the command then behaved.
  # With it, 1 is docket choosing its own exit code and 137 is timeout having
  # to SIGKILL a process that ignored the interrupt.
  run timeout --preserve-status --kill-after=8 --signal=INT 2 \
    env DOKKU_HOST="$HANG_HOST" "$(docket_bin)" apply --tasks "$TASKS_FILE"

  assert_output --partial "run cancelled"
  [ "$status" -eq 1 ]
  refute_output --partial "second"
}

@test "SIGINT aborts the whole plan" {
  run timeout --preserve-status --kill-after=8 --signal=INT 2 \
    env DOKKU_HOST="$HANG_HOST" "$(docket_bin)" plan --tasks "$TASKS_FILE"

  assert_output --partial "run cancelled"
  [ "$status" -eq 1 ]
  refute_output --partial "second"
}
