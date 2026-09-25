#!/usr/bin/env bats

load test_helper

# These tests put a fake `ssh` first on PATH, so they need neither a remote
# host nor a local dokku. The fake stands in for what OpenSSH does with a
# control socket: the first call for a ControlPath creates it, and
# `ssh -O exit` removes it. A socket still on disk after docket exits is a
# master docket never closed. Every remote command answers with exit 0 and no
# output, which the dokku_app probe reads as "the app exists".

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
socket=""
exit_master=0
host=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o)
      case "$2" in ControlPath=*) socket="${2#ControlPath=}" ;; esac
      shift 2
      ;;
    -O)
      [ "$2" = "exit" ] && exit_master=1
      shift 2
      ;;
    -p) shift 2 ;;
    --) shift; break ;;
    *) host="$1"; shift ;;
  esac
done
if [ "$exit_master" -eq 1 ]; then
  echo "exit $host" >>"$SSH_LOG"
  rm -f "$socket"
  exit 0
fi
echo "run $host" >>"$SSH_LOG"
touch "$socket"
exit 0
EOF
  chmod +x "$FAKE_BIN/ssh"
  export SSH_LOG
}

teardown() {
  rm -rf "${SOCKET_DIR:-}"
}

# run_fake_ssh runs docket with the fake ssh first on PATH and its sockets in
# SOCKET_DIR.
run_fake_ssh() {
  PATH="$FAKE_BIN:$PATH" TMPDIR="$SOCKET_DIR" run "$(docket_bin)" "$@"
}

assert_no_sockets() {
  run find "$SOCKET_DIR" -name 'docket-*.sock'
  assert_success
  assert_output ""
}

two_host_recipe() {
  write_tasks_file <<EOF
---
- name: first server
  tasks:
    - dokku_app:
        app: docket-test-session
- name: second server
  host: deploy@two.example.com
  tasks:
    - dokku_app:
        app: docket-test-session
EOF
}

@test "ssh session: apply closes the connection to every host it reached" {
  two_host_recipe
  DOKKU_HOST=deploy@one.example.com run_fake_ssh apply --tasks "$TASKS_FILE"
  assert_success
  run cat "$SSH_LOG"
  assert_line "run deploy@one.example.com"
  assert_line "run deploy@two.example.com"
  assert_line "exit deploy@one.example.com"
  assert_line "exit deploy@two.example.com"
  assert_no_sockets
}

@test "ssh session: plan closes the connection to every host it reached" {
  two_host_recipe
  DOKKU_HOST=deploy@one.example.com run_fake_ssh plan --tasks "$TASKS_FILE"
  assert_success
  run cat "$SSH_LOG"
  assert_line "exit deploy@one.example.com"
  assert_line "exit deploy@two.example.com"
  assert_no_sockets
}

@test "ssh session: a host no play reaches is not closed" {
  # The run-wide host is never dispatched to: the only play sends its tasks
  # elsewhere. The session records where commands actually went, so nothing
  # is opened or closed for it.
  write_tasks_file <<EOF
---
- name: second server
  host: deploy@two.example.com
  tasks:
    - dokku_app:
        app: docket-test-session
EOF
  DOKKU_HOST=deploy@one.example.com run_fake_ssh plan --tasks "$TASKS_FILE"
  assert_success
  run cat "$SSH_LOG"
  assert_line "exit deploy@two.example.com"
  refute_line --partial "deploy@one.example.com"
  assert_no_sockets
}

@test "ssh session: export closes its connection" {
  # The fake answers every command with empty output, so what export makes of
  # the server is beside the point; the connection it opened must be closed
  # whichever way the command exits.
  DOKKU_HOST=deploy@one.example.com run_fake_ssh export --app docket-test-session --output -
  run cat "$SSH_LOG"
  assert_line "run deploy@one.example.com"
  assert_line "exit deploy@one.example.com"
  assert_no_sockets
}
