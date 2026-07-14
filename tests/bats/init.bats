#!/usr/bin/env bats

load test_helper

setup() {
  docket_build
}

@test "docket init writes tasks.yml" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init
  assert_success
  assert [ -f tasks.yml ]
  assert_output --partial "Created tasks.yml"
}

@test "docket init refuses to overwrite without --force" {
  cd "$BATS_TEST_TMPDIR"
  echo "preserved" >tasks.yml
  run "$(docket_bin)" init
  assert_failure
  assert_output --partial "already exists"
  run cat tasks.yml
  assert_output "preserved"
}

@test "docket init --force overwrites" {
  cd "$BATS_TEST_TMPDIR"
  echo "preserved" >tasks.yml
  run "$(docket_bin)" init --force
  assert_success
  run cat tasks.yml
  refute_output "preserved"
  assert_output --partial "dokku_app"
}

@test "docket init --name uses the supplied name" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init --name billing
  assert_success
  run cat tasks.yml
  assert_output --partial "default: billing"
}

@test "docket init --repo uses the supplied url" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init --repo "git@example.com:foo/bar.git"
  assert_success
  run cat tasks.yml
  assert_output --partial 'default: "git@example.com:foo/bar.git"'
  refute_output --partial "required: true"
}

@test "docket init defaults the play name to the cwd basename" {
  mkdir -p "$BATS_TEST_TMPDIR/widget-svc"
  cd "$BATS_TEST_TMPDIR/widget-svc"
  run "$(docket_bin)" init
  assert_success
  run cat tasks.yml
  assert_output --partial "default: widget-svc"
}

@test "docket init picks up remote.origin.url from .git/config" {
  cd "$BATS_TEST_TMPDIR"
  mkdir -p .git
  cat >.git/config <<CFG
[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@example.com:owner/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
CFG
  run "$(docket_bin)" init
  assert_success
  run cat tasks.yml
  assert_output --partial 'default: "git@example.com:owner/repo.git"'
}

@test "docket init --name with YAML-special characters writes a valid scaffold" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init --name '@web'
  assert_success
  assert [ -f tasks.yml ]
  run cat tasks.yml
  assert_output --partial "default: '@web'"

  run "$(docket_bin)" validate --tasks tasks.yml
  assert_success
  assert_output --partial "is valid"
}

@test "docket init defaults an unquoted name for ordinary basenames" {
  mkdir -p "$BATS_TEST_TMPDIR/plain-svc"
  cd "$BATS_TEST_TMPDIR/plain-svc"
  run "$(docket_bin)" init
  assert_success
  run cat tasks.yml
  assert_output --partial "default: plain-svc"
}

@test "docket init --output - writes to stdout and creates no file" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init --output -
  assert_success
  assert_output --partial "dokku_app"
  refute_output --partial "Created"
  assert [ ! -f tasks.yml ]
}

@test "docket init output validates" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  run "$(docket_bin)" validate
  assert_success
  assert_output --partial "is valid"
}

@test "docket init --minimal writes minimal scaffold" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init --minimal
  assert_success
  run cat tasks.yml
  assert_output --partial "dokku_app"
  refute_output --partial "dokku_git_sync"
  refute_output --partial "inputs:"
}

@test "docket init --name sets the play name so --play resolves" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init --name web --minimal
  assert_success

  run cat tasks.yml
  assert_output --partial "name: web"

  # The scaffolded play is named after --name (not the legacy auto-name
  # "tasks"), so --play web resolves after init. --minimal avoids the
  # default template's required repo input, which is enforced before the
  # --list-tasks branch.
  run "$(docket_bin)" plan --tasks tasks.yml --play web --list-tasks
  assert_success
  assert_output --partial "create app"
}
