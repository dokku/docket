#!/usr/bin/env bats

load test_helper

# help.bats covers the `--help` page a recipe's own inputs contribute to.
# Nothing here contacts a server: FlagSet() reads the recipe, registers a flag
# per input, and CommandHelp renders the usage strings, all before Run.
#
# The masking cases are #490. `--help` is the one stream docket writes with an
# empty mask registry - it is rendered before any recipe is parsed - so a
# `sensitive: true` input's `default:` is masked by rewriting the flag's
# advertised default rather than by MaskString at the print site. The Go tests
# in commands/sensitive_default_masking_test.go call Help() directly; these
# drive the real binary through the CLI dispatcher, which prints help on stderr
# and exits 0.

setup() {
  docket_build
}

@test "apply --help masks a sensitive input's default value" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: token, default: helpdefaultzzz, sensitive: true }
  tasks:
    - dokku_app: { app: web }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --help
  assert_success
  assert_output --partial "--token"
  refute_output --partial "helpdefaultzzz"
  assert_output --partial '(default "***")'
}

@test "apply --help still shows an ordinary input's default" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: app, default: web }
    - { name: token, required: true, sensitive: true }
  tasks:
    - dokku_app: { app: "{{ .app }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --help
  assert_success
  assert_output --partial '(default "web")'
  refute_output --partial '(default "***")'
}
