#!/usr/bin/env bats

load test_helper

# install.sh is exercised offline: a fake `curl` first on PATH logs every URL
# it is asked for, answers the latest-release lookup, and serves a download
# only when the URL names a tag listed in FAKE_CURL_TAGS - the tags that
# exist, in the old X.Y.Z form or the vX.Y.Z form releases use from 0.9.0 on.

setup() {
  INSTALL_SH="$BATS_TEST_DIRNAME/../../install.sh"
  FAKE_BIN="$BATS_TEST_TMPDIR/fake-bin"
  CURL_LOG="$BATS_TEST_TMPDIR/curl.log"
  BIN_DIR="$BATS_TEST_TMPDIR/bin"
  export CURL_LOG
  mkdir -p "$FAKE_BIN"
  : >"$CURL_LOG"

  cat >"$FAKE_BIN/curl" <<'EOF'
#!/usr/bin/env bash
url=""
out=""
while [ $# -gt 0 ]; do
  case "$1" in
  -o) out="$2"; shift 2 ;;
  -*) shift ;;
  *) url="$1"; shift ;;
  esac
done
echo "$url" >>"$CURL_LOG"

case "$url" in
*/releases/latest)
  echo '  "tag_name": "v0.9.0",'
  exit 0
  ;;
*/releases/download/*)
  tag="${url#*/releases/download/}"
  tag="${tag%%/*}"
  for known in $FAKE_CURL_TAGS; do
    if [ "$tag" = "$known" ]; then
      printf '#!/bin/sh\necho %s\n' "$tag" >"$out"
      exit 0
    fi
  done
  echo "curl: (22) The requested URL returned error: 404" >&2
  exit 22
  ;;
esac
exit 1
EOF
  chmod +x "$FAKE_BIN/curl"
}

run_install() {
  run env PATH="$FAKE_BIN:$PATH" BIN_DIR="$BIN_DIR" CURL_LOG="$CURL_LOG" \
    FAKE_CURL_TAGS="0.8.0 v0.9.0" "$@" sh "$INSTALL_SH"
}

@test "install.sh installs an unprefixed tag as given" {
  run_install VERSION=0.8.0
  assert_success
  assert_output --partial "installed docket 0.8.0"
  run grep -c "/releases/download/" "$CURL_LOG"
  assert_output "1"
  run cat "$CURL_LOG"
  assert_output --partial "/releases/download/0.8.0/docket-"
  run "$BIN_DIR/docket"
  assert_output "0.8.0"
}

@test "install.sh installs a v-prefixed tag as given" {
  run_install VERSION=v0.9.0
  assert_success
  run cat "$CURL_LOG"
  assert_output --partial "/releases/download/v0.9.0/docket-"
  run "$BIN_DIR/docket"
  assert_output "v0.9.0"
}

@test "install.sh retries a bare version with the v prefix" {
  run_install VERSION=0.9.0
  assert_success
  run cat "$CURL_LOG"
  assert_line --index 0 --partial "/releases/download/0.9.0/docket-"
  assert_line --index 1 --partial "/releases/download/v0.9.0/docket-"
  run "$BIN_DIR/docket"
  assert_output "v0.9.0"
}

@test "install.sh does not retry a v-prefixed tag that does not exist" {
  run_install VERSION=v0.10.0
  assert_failure
  run grep -c "/releases/download/" "$CURL_LOG"
  assert_output "1"
  [ ! -e "$BIN_DIR/docket" ]
}

@test "install.sh fails when neither spelling of a version exists" {
  run_install VERSION=0.10.0
  assert_failure
  run grep -c "/releases/download/" "$CURL_LOG"
  assert_output "2"
  [ ! -e "$BIN_DIR/docket" ]
}

@test "install.sh installs the latest release's tag" {
  run_install
  assert_success
  run cat "$CURL_LOG"
  assert_line --index 0 --partial "/releases/latest"
  assert_line --index 1 --partial "/releases/download/v0.9.0/docket-"
}
