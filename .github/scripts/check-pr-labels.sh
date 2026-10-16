#!/usr/bin/env bash
# Fails unless the pull request carries a label that picks its section in the
# release notes. PR_LABELS is a JSON array of the pull request's label names.
set -euo pipefail

CATEGORY_LABELS=(breaking-change enhancement bug refactor documentation tests dependencies)

main() {
  local label
  for label in "${CATEGORY_LABELS[@]}"; do
    if jq -e --arg label "$label" 'index($label) != null' <<<"${PR_LABELS:-[]}" >/dev/null; then
      echo "Found release notes label: $label"
      return 0
    fi
  done

  echo "This pull request needs one of these labels to place it in the release notes:" >&2
  printf '  %s\n' "${CATEGORY_LABELS[@]}" >&2
  return 1
}

main "$@"
