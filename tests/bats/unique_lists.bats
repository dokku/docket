#!/usr/bin/env bats

load test_helper

# docket schema and docket validate are offline, so nothing here calls
# require_dokku.

setup() {
  docket_build
}

@test "docket schema declares the lists whose members are unique (#648)" {
  run "$(docket_bin)" schema
  assert_success

  echo "$output" |
    jq -e '[.tasks[] | .type as $t | .fields[] | select(.unique == true) | "\($t).\(.name)"] | sort == ["dokku_config.preserve", "dokku_scheduler_k3s_profile.kubelet_args", "dokku_storage_mount.phases"]' >/dev/null ||
    fail "the task-level unique lists are not preserve, kubelet_args and phases"
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_storage_mount") | .fields[] | select(.name == "mounts") | .item.fields[] | select(.name == "phases") | .unique == true' >/dev/null ||
    fail "dokku_storage_mount mounts[].phases is not marked unique"

  # unique is only ever true, only on a list, and never on an identity
  # collection.
  echo "$output" |
    jq -e '[.. | objects | select(has("name") and has("type") and has("required")) | select(has("unique"))] | all(.unique == true and .type == "list" and .identity != "collection")' >/dev/null ||
    fail "unique appears somewhere other than a non-collection list"

  # Every unordered list is a set: an identity collection or unique.
  echo "$output" |
    jq -e '[.. | objects | select(has("name") and has("type") and has("required")) | select(.type == "list" and .ordered == false)] | length > 0 and all((.identity == "collection") or (.unique == true))' >/dev/null ||
    fail "an unordered list is neither an identity collection nor unique"

  run "$(docket_bin)" schema --task dokku_scheduler_k3s_profile
  assert_success
  echo "$output" |
    jq -e '.tasks[0].fields[] | select(.name == "kubelet_args") | .unique == true and .note.suffix == " (no duplicates)"' >/dev/null ||
    fail "a narrowed catalog drops unique or its note"
}

@test "docket validate rejects a repeated member of a unique list (#648)" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_scheduler_k3s_profile:
        name: edge
        role: worker
        kubelet_args:
          - max-pods=100
          - max-pods=100
    - dokku_storage_mount:
        app: docket-test-unique
        state: set
        mounts:
          - entry_name: data
            container_dir: /data
            phases: [run, run]
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"field":"kubelet_args[1]"'
  assert_output --partial '"field":"mounts[0].phases[1]"'
  assert_output --partial "'kubelet_args[1]' repeats 'kubelet_args[0]'"
}

@test "docket validate accepts one kubelet arg key with two values (#648)" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_scheduler_k3s_profile:
        name: edge
        role: worker
        kubelet_args:
          - max-pods=100
          - max-pods=110
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
}
