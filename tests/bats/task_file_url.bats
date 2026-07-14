#!/usr/bin/env bats

load test_helper

setup() {
  docket_build
}

teardown() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
}

# docket apply / plan accept an http(s):// --tasks URL, fetched over HTTP.
# This drives the shipped binary against a local HTTP server; --list-tasks
# reads and parses the recipe without contacting a Dokku server, so the
# served recipe just needs a real task type and no required inputs.
@test "docket plan fetches a recipe from an http URL" {
  command -v python3 >/dev/null 2>&1 || skip "python3 not available"

  mkdir -p "$BATS_TEST_TMPDIR/www"
  cat >"$BATS_TEST_TMPDIR/www/tasks.yml" <<'EOF'
---
- tasks:
    - name: create app
      dokku_app:
        app: url-recipe
        state: present
EOF

  # Bind an ephemeral port and write it out so the test never collides
  # with a fixed port already in use on the runner.
  python3 - "$BATS_TEST_TMPDIR/www" "$BATS_TEST_TMPDIR/port" <<'PY' &
import http.server, socketserver, sys, os
os.chdir(sys.argv[1])
with socketserver.TCPServer(("127.0.0.1", 0), http.server.SimpleHTTPRequestHandler) as httpd:
    with open(sys.argv[2], "w") as f:
        f.write(str(httpd.server_address[1]))
    httpd.serve_forever()
PY
  SERVER_PID=$!

  for _ in $(seq 1 50); do
    [ -s "$BATS_TEST_TMPDIR/port" ] && break
    sleep 0.1
  done
  [ -s "$BATS_TEST_TMPDIR/port" ] || { echo "http server did not start"; return 1; }
  port="$(cat "$BATS_TEST_TMPDIR/port")"

  run "$(docket_bin)" plan --tasks "http://127.0.0.1:${port}/tasks.yml" --list-tasks
  assert_success
  assert_output --partial "create app"
}

@test "docket plan reports a clear error for a 404 recipe URL" {
  command -v python3 >/dev/null 2>&1 || skip "python3 not available"

  python3 - "$BATS_TEST_TMPDIR/port" <<'PY' &
import http.server, socketserver, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_error(404, "not found")
    def log_message(self, *a):
        pass
with socketserver.TCPServer(("127.0.0.1", 0), H) as httpd:
    with open(sys.argv[1], "w") as f:
        f.write(str(httpd.server_address[1]))
    httpd.serve_forever()
PY
  SERVER_PID=$!

  for _ in $(seq 1 50); do
    [ -s "$BATS_TEST_TMPDIR/port" ] && break
    sleep 0.1
  done
  [ -s "$BATS_TEST_TMPDIR/port" ] || { echo "http server did not start"; return 1; }
  port="$(cat "$BATS_TEST_TMPDIR/port")"

  run "$(docket_bin)" plan --tasks "http://127.0.0.1:${port}/missing.yml" --list-tasks
  assert_failure
  assert_output --partial "unexpected status"
}
