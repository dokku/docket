#!/usr/bin/env bats
#
# Shell-completion regression tests for #340. docket enables autocompletion by
# default (mitchellh/cli NewCLI sets Autocomplete: true) and satisfies the shell
# completion protocol when COMP_LINE is set, so completing a recipe-file flag or
# argument runs the real predictor. The previous brace glob
# "*.{yml,yaml,json,json5}" matched no file through Go's filepath.Glob, so only
# directories were ever offered; these tests assert real recipe files come back.
#
# COMP_POINT is intentionally omitted: posener/complete falls back to the end of
# COMP_LINE, so the trailing space makes the word being completed empty and every
# candidate file matches.

load test_helper

setup() {
  docket_build
}

@test "docket apply --tasks completes recipe files but not other files (#340)" {
  cd "$BATS_TEST_TMPDIR"
  : >tasks.yml
  : >config.yaml
  : >data.json
  : >recipe.json5
  : >notes.txt
  export COMP_LINE='docket apply --tasks '
  run "$(docket_bin)" apply --tasks
  assert_success
  assert_output --partial 'tasks.yml'
  assert_output --partial 'config.yaml'
  assert_output --partial 'data.json'
  assert_output --partial 'recipe.json5'
  refute_output --partial 'notes.txt'
}

@test "docket fmt completes recipe files positionally (#340)" {
  cd "$BATS_TEST_TMPDIR"
  : >tasks.yml
  : >notes.txt
  export COMP_LINE='docket fmt '
  run "$(docket_bin)" fmt
  assert_success
  assert_output --partial 'tasks.yml'
  refute_output --partial 'notes.txt'
}
