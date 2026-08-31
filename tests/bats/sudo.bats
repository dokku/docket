#!/usr/bin/env bats

load test_helper

APP="docket-test-sudo"

# require_passwordless_sudo skips unless `sudo -n` works without a prompt.
# --sudo asks for exactly that, so on a box where sudo wants a password there
# is nothing to assert but a failure.
require_passwordless_sudo() {
  require_dokku
  if ! sudo -n true >/dev/null 2>&1; then
    skip "passwordless sudo not available"
  fi
}

setup() {
  require_passwordless_sudo
  docket_build
  dokku_clean_app "$APP"
}

teardown() {
  if command -v dokku >/dev/null 2>&1; then
    dokku_clean_app "$APP"
  fi
}

# write_app_recipe writes the one-task recipe every case here applies. The task
# has to actually run for --verbose to echo anything, which is why setup
# destroys the app first.
write_app_recipe() {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure $APP
      dokku_app:
        app: $APP
EOF
}

@test "--sudo elevates a local dokku invocation" {
  write_app_recipe

  # --sudo used to be a no-op without --host: only the SSH argv builder read
  # it, so a local run silently ignored the flag the name promises.
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --sudo --verbose
  assert_success
  assert_output --partial "sudo -n -u root dokku"
  run dokku apps:exists "$APP"
  assert_success
}

@test "a local run without --sudo is not elevated" {
  write_app_recipe

  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --verbose
  assert_success
  assert_output --partial "dokku --quiet apps:create $APP"
  refute_output --partial "sudo -n -u root"
}

@test "DOKKU_SUDO=1 is equivalent to --sudo" {
  write_app_recipe

  # The env var is documented as the flag's equivalent, and used to reach the
  # transport by a different route - read straight out of the process
  # environment when the ssh argv was built. Now that the commands layer is its
  # only reader, this is what keeps it working.
  DOKKU_SUDO=1 run "$(docket_bin)" apply --tasks "$TASKS_FILE" --verbose
  assert_success
  assert_output --partial "sudo -n -u root dokku"
}

@test "--sudo applies only to the run that asked for it" {
  write_app_recipe

  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --sudo --verbose
  assert_success
  assert_output --partial "sudo -n -u root dokku"

  # The setting used to be written into the process environment and never
  # cleared, which is what ruled out two differently-configured runs in one
  # process. Separate processes here, so this held before too - it is the
  # observable end of the change, kept honest from the outside.
  dokku_clean_app "$APP"
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --verbose
  assert_success
  refute_output --partial "sudo -n -u root"
}
