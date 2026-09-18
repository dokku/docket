#!/usr/bin/env bats

load test_helper

# task_file_hcl.bats covers #407's HCL recipe end to end against the docket
# binary: the block mapping, the diagnostics, format detection from an
# extension and from stdin, and an HCL vars-file.
#
# Nothing here reaches a server. validate never does, and `apply --list-tasks`
# resolves the plan offline and returns before planning.

setup() {
  docket_build
}

@test "docket validate accepts a tasks.hcl" {
  write_tasks_file tasks.hcl <<'EOF'
play "web" {
  input "app" {
    default = "docket-test-hcl"
  }

  dokku_app "create app" {
    app = "{{ .app | dq }}"
  }
}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate accepts every hcl comment syntax" {
  write_tasks_file tasks.hcl <<'EOF'
# a hash comment
// a line comment
/* a block comment */
play {
  dokku_app { # beside the header
    app = "api"
  }
}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate reports hcl_parse on malformed HCL" {
  write_tasks_file tasks.hcl <<'EOF'
play "web" {
  dokku_app "create"
}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"hcl_parse"'
}

@test "docket validate reports duplicate_key on a repeated HCL attribute" {
  write_tasks_file tasks.hcl <<'EOF'
play "web" {
  dokku_app "create" {
    app = "a"
    app = "b"
  }
}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"duplicate_key"'
}

@test "docket validate refuses an HCL expression" {
  write_tasks_file tasks.hcl <<'EOF'
play "web" {
  dokku_app "create" {
    app = upper("api")
  }
}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "docket recipes are data"
}

@test "docket apply --list-tasks works against tasks.hcl" {
  write_tasks_file tasks.hcl <<'EOF'
play {
  dokku_app "first task" {
    app = "api"
  }

  dokku_config "second task" {
    app = "api"
    config = {
      K = "v"
    }
  }
}
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "first task"
  assert_output --partial "second task"
}

@test "docket apply --list-tasks reads a group entry written as a task block" {
  write_tasks_file tasks.hcl <<'EOF'
play {
  task "deploy" {
    block {
      dokku_app "inner" {
        app = "api"
      }
    }

    rescue {
      dokku_app "handler" {
        app = "api-rescue"
      }
    }
  }
}
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "inner"
}

@test "docket auto-detects tasks.hcl when no --tasks flag is given" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.hcl <<'EOF'
play {
  dokku_app "auto-detected" {
    app = "api"
  }
}
EOF
  run "$(docket_bin)" validate
  assert_success
  assert_output --partial "tasks.hcl"
  assert_output --partial "is valid"
}

@test "docket prefers tasks.yml over tasks.hcl when both exist" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
    - name: yaml-task
      dokku_app:
        app: api
EOF
  cat >tasks.hcl <<'EOF'
play {
  dokku_app "hcl-task" {
    app = "api"
  }
}
EOF
  run "$(docket_bin)" apply --list-tasks
  assert_success
  assert_output --partial "yaml-task"
  refute_output --partial "hcl-task"
  assert_output --partial "tasks.yml, tasks.hcl both exist"
}

@test "docket sniffs an HCL recipe piped to stdin" {
  run bash -c 'cat <<EOF | "$0" validate -
play {
  dokku_app "piped" {
    app = "api"
  }
}
EOF' "$(docket_bin)"
  assert_success
  assert_output --partial "is valid"
}

@test "a slash-commented HCL recipe on stdin needs --tasks-format hcl" {
  run bash -c 'cat <<EOF | "$0" validate -
// a recipe
play {
  dokku_app "piped" {
    app = "api"
  }
}
EOF' "$(docket_bin)"
  assert_failure

  run bash -c 'cat <<EOF | "$0" validate --tasks-format hcl -
// a recipe
play {
  dokku_app "piped" {
    app = "api"
  }
}
EOF' "$(docket_bin)"
  assert_success
  assert_output --partial "is valid"
}

@test "--tasks-format hcl overrides a misleading extension" {
  write_tasks_file recipe.txt <<'EOF'
play {
  dokku_app "create" {
    app = "api"
  }
}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --tasks-format hcl
  assert_success
  assert_output --partial "is valid"
}

# The two tests below observe the SUBSTITUTED value through the task name,
# which --list-tasks prints. The whole file is rendered as text before it is
# parsed, so a label is substituted the same way an attribute is, and the name
# is the one resolved value an offline run reports.

@test "docket apply --list-tasks reads an HCL --vars-file" {
  write_tasks_file tasks.hcl <<'EOF'
play {
  input "app" {
    default = "fallback"
  }

  dokku_app "create {{ .app | dq }}" {
    app = "{{ .app | dq }}"
  }
}
EOF
  cat >"$BATS_TEST_TMPDIR/prod.hcl" <<'EOF'
# production values
app = "from-vars-file"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --vars-file "$BATS_TEST_TMPDIR/prod.hcl"
  assert_success
  assert_output --partial "create from-vars-file"
  refute_output --partial "fallback"
}

@test "an input value carrying HCL's own template syntax survives dq" {
  write_tasks_file tasks.hcl <<'EOF'
play {
  input "app" {
    default = "fallback"
  }

  dokku_app "create {{ .app | dq }}" {
    app = "{{ .app | dq }}"
  }
}
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --app '${literal}'
  assert_success
  assert_output --partial 'create ${literal}'
}

@test "an input value carrying a double quote survives dq in HCL" {
  write_tasks_file tasks.hcl <<'EOF'
play {
  input "app" {
    default = "fallback"
  }

  dokku_app "create {{ .app | dq }}" {
    app = "{{ .app | dq }}"
  }
}
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --app 'we"b'
  assert_success
  assert_output --partial 'create we"b'
}
