#!/usr/bin/env bats

load test_helper

# These tests put a fake `ssh` first on PATH that dies from a signal instead of
# answering, so they need neither a remote host nor a local dokku. They cover
# the half of an interrupt cancel.bats can only hit by luck: a terminal's
# Ctrl-C (or timeout(1)) signals the whole process group, and the `ssh` child
# can die from it before docket's own handler has cancelled the run. A child
# killed by a signal never answered, so its exit status must not be read as the
# server saying "the app is missing".
#
# FAKE_SSH_MODE picks what the fake does on a remote command:
#
#   group  kill itself, then send docket SIGINT 200ms later - the losing order
#          of the race, made deterministic
#   self   kill itself only; docket is never interrupted
#
# The fake kills itself with SIGTERM rather than SIGINT because a shell can
# inherit SIGINT as ignored, and a fake that shrugged off its own signal would
# exit 0 and prove nothing. docket sees a signalled child either way.

setup() {
  docket_build
  # Under /tmp rather than $BATS_TEST_TMPDIR: a unix socket path is capped at
  # around 104 bytes on macOS, and docket puts its sockets under TMPDIR.
  SOCKET_DIR="$(mktemp -d /tmp/docket-sock.XXXXXX)"
  FAKE_BIN="$BATS_TEST_TMPDIR/fake-bin"
  SSH_LOG="$BATS_TEST_TMPDIR/ssh.log"
  mkdir -p "$FAKE_BIN"
  : >"$SSH_LOG"
  cat >"$FAKE_BIN/ssh" <<'EOF'
#!/usr/bin/env bash
while [ $# -gt 0 ]; do
  case "$1" in
    -o | -p) shift 2 ;;
    -O) exit 0 ;;
    --) shift; break ;;
    *) shift ;;
  esac
done
echo "run $*" >>"$SSH_LOG"
if [ "$FAKE_SSH_MODE" = "group" ]; then
  docket=$PPID
  (sleep 0.2; kill -INT "$docket") >/dev/null 2>&1 &
fi
kill -TERM $$
EOF
  chmod +x "$FAKE_BIN/ssh"
  export SSH_LOG

  # Two plays, not two tasks in one: an error already ends the play it happened
  # in under apply, so only a second play shows whether the run went on.
  write_tasks_file <<EOF
---
- name: one
  tasks:
    - name: first
      dokku_app:
        app: docket-test-race-one
- name: two
  tasks:
    - name: second
      dokku_app:
        app: docket-test-race-two
EOF
}

teardown() {
  rm -rf "${SOCKET_DIR:-}"
}

# run_fake_ssh runs docket against a host with the fake ssh first on PATH, its
# sockets in SOCKET_DIR, and the given FAKE_SSH_MODE.
run_fake_ssh() {
  local mode="$1"
  shift
  FAKE_SSH_MODE="$mode" DOKKU_HOST=deploy@race.example.com PATH="$FAKE_BIN:$PATH" TMPDIR="$SOCKET_DIR" \
    run "$(docket_bin)" "$@"
}

@test "interrupt race: plan does not read a killed probe as the app missing" {
  run_fake_ssh group plan --tasks "$TASKS_FILE"
  assert_failure 1
  assert_output --partial "run cancelled"
  refute_output --partial "app missing"
  refute_output --partial "second"
}

@test "interrupt race: apply stops without creating anything" {
  run_fake_ssh group apply --tasks "$TASKS_FILE"
  assert_failure 1
  assert_output --partial "run cancelled"
  refute_output --partial "second"
  run cat "$SSH_LOG"
  assert_line --partial "apps:exists"
  refute_output --partial "apps:create"
}

@test "interrupt race: a probe killed outside an interrupt is an error, not an answer" {
  run_fake_ssh self plan --tasks "$TASKS_FILE"
  assert_failure 1
  refute_output --partial "run cancelled"
  refute_output --partial "app missing"
  assert_output --partial "killed by a signal"
  assert_output --partial "second"
}
