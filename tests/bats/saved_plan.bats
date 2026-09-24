#!/usr/bin/env bats

load test_helper

# saved_plan.bats covers `docket plan --output` and `docket apply --plan`
# (#408). The round trip needs a server to probe, so those tests require dokku
# one by one; the flag and file checks run before anything is probed, so they
# run without it.

setup() {
  docket_build
  dokku_clean_app docket-test-saved-plan
}

teardown() {
  dokku_clean_app docket-test-saved-plan
}

write_saved_plan_recipe() {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-saved-plan
      dokku_app:
        app: docket-test-saved-plan
EOF
}

@test "docket apply --plan applies a saved plan" {
  require_dokku
  write_saved_plan_recipe
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --output "$BATS_TEST_TMPDIR/plan.json"
  assert_success
  assert_output --partial "[+]"
  assert_output --partial "Saved plan to"
  assert_equal "$(file_mode "$BATS_TEST_TMPDIR/plan.json")" "600"

  # The saved plan carries its own copy of the recipe.
  rm "$TASKS_FILE"
  run "$(docket_bin)" apply --plan "$BATS_TEST_TMPDIR/plan.json"
  assert_success
  assert_output --partial "[changed]"
  run dokku apps:exists docket-test-saved-plan
  assert_success
}

@test "docket apply --plan refuses a stale plan" {
  require_dokku
  write_saved_plan_recipe
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --output "$BATS_TEST_TMPDIR/plan.json"
  assert_success

  # Someone else creates the app between plan and apply, so the saved [+] no
  # longer describes what apply would do.
  dokku apps:create docket-test-saved-plan

  run "$(docket_bin)" apply --plan "$BATS_TEST_TMPDIR/plan.json"
  assert_failure
  assert_output --partial "saved plan is stale"
  assert_output --partial 'tasks/ensure docket-test-saved-plan: planned "+'
  refute_output --partial "[changed]"
}

@test "docket plan --output refuses to overwrite a file without --force" {
  write_saved_plan_recipe
  echo keep >"$BATS_TEST_TMPDIR/plan.json"
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --output "$BATS_TEST_TMPDIR/plan.json"
  assert_failure
  assert_output --partial "already exists; pass --force to overwrite"
  assert_equal "$(cat "$BATS_TEST_TMPDIR/plan.json")" "keep"
}

@test "docket plan --output refuses to stream to stdout" {
  write_saved_plan_recipe
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --output -
  assert_failure
  assert_output --partial "--output cannot be -"
}

@test "docket apply --plan refuses flags the saved plan already fixes" {
  run "$(docket_bin)" apply --plan "$BATS_TEST_TMPDIR/plan.json" --tags deploy
  assert_failure
  assert_output --partial "--plan cannot be used with --tags"
}

@test "docket apply --plan refuses a file that is not a saved plan" {
  echo '{"format": "something-else"}' >"$BATS_TEST_TMPDIR/plan.json"
  run "$(docket_bin)" apply --plan "$BATS_TEST_TMPDIR/plan.json"
  assert_failure
  assert_output --partial "is not a docket saved plan"
}
