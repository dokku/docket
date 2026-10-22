#!/usr/bin/env bash
# Installs dokku and the plugins docket's integration tests run against. The
# setup-dokku action calls it with its inputs in the environment:
#
#   DOKKU_VERSION        the dokku package version to install
#   DOKKU_PLUGINS        plugin names from PLUGIN_PINS, separated by commas,
#                        spaces or newlines; empty installs none
#   DOCKER_LEGACY_LINKS  true to keep Docker's legacy link env vars
#   INITIALIZE_K3S       true to run scheduler-k3s:initialize
set -euo pipefail

# Each plugin docket's tasks need, as "name repository committish". The
# committish is the release docket's integration suite is tested against.
PLUGIN_PINS=(
  "redis https://github.com/dokku/dokku-redis.git 2.3.1"
  "http-auth https://github.com/dokku/dokku-http-auth.git 0.14.0"
  "global-cert https://github.com/dokku-community/dokku-global-cert.git 0.7.0"
  "letsencrypt https://github.com/dokku/dokku-letsencrypt.git 0.25.2"
  "acl https://github.com/dokku-community/dokku-acl.git 2.0.1"
  "maintenance https://github.com/dokku/dokku-maintenance.git 0.10.2"
)

log() {
  echo "=====> $*"
}

fail() {
  echo " !     $*" >&2
  exit 1
}

# Prints the value of a true/false input, failing on anything else.
parse_bool() {
  local name="$1" value="$2"
  case "$value" in
  true | false) echo "$value" ;;
  *) fail "$name must be true or false, got '$value'" ;;
  esac
}

# Prints the pin line for a plugin name, failing when docket does not pin it.
plugin_pin() {
  local name="$1" pin
  for pin in "${PLUGIN_PINS[@]}"; do
    if [[ "${pin%% *}" == "$name" ]]; then
      echo "$pin"
      return 0
    fi
  done

  local known=()
  for pin in "${PLUGIN_PINS[@]}"; do
    known+=("${pin%% *}")
  done
  fail "unknown plugin '$name'; expected one of: ${known[*]}"
}

enable_legacy_links() {
  log "Enabling Docker legacy link env vars"
  sudo mkdir -p /etc/systemd/system/docker.service.d
  printf '[Service]\nEnvironment="DOCKER_KEEP_DEPRECATED_LEGACY_LINKS_ENV_VARS=1"\n' | sudo tee /etc/systemd/system/docker.service.d/legacy-links.conf
  sudo systemctl daemon-reload
  sudo systemctl restart docker
}

install_dokku() {
  local version="$1"
  log "Installing dokku $version"
  sudo mkdir -p /etc/apt/keyrings
  curl -fsSL https://packagecloud.io/dokku/dokku/gpgkey | sudo gpg --dearmor --yes -o /etc/apt/keyrings/dokku-archive-keyring.gpg
  echo "deb [signed-by=/etc/apt/keyrings/dokku-archive-keyring.gpg] https://packagecloud.io/dokku/dokku/ubuntu/ $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/dokku.list
  sudo apt-get update -qq
  sudo DEBIAN_FRONTEND=noninteractive DEBCONF_NONINTERACTIVE_SEEN=true apt-get install -qq -y "dokku=$version"
  sudo dokku plugin:install-dependencies --core
}

install_plugin() {
  local name repository committish
  read -r name repository committish <<<"$1"
  log "Installing dokku $name plugin $committish"
  sudo dokku plugin:install "$repository" --committish "$committish" "$name"
}

main() {
  local version="${DOKKU_VERSION:-}"
  local legacy_links k3s
  legacy_links="$(parse_bool docker-legacy-links "${DOCKER_LEGACY_LINKS:-true}")"
  k3s="$(parse_bool initialize-k3s "${INITIALIZE_K3S:-false}")"
  [[ -n "$version" ]] || fail "dokku-version must not be empty"

  # Resolve every plugin before installing anything, so a typo fails fast.
  local names=() plugins=() name
  read -r -a names <<<"$(tr ',\n' '  ' <<<"${DOKKU_PLUGINS:-}")"
  for name in "${names[@]}"; do
    plugins+=("$(plugin_pin "$name")")
  done

  if [[ "$legacy_links" == "true" ]]; then
    enable_legacy_links
  fi

  install_dokku "$version"

  local pin
  for pin in "${plugins[@]}"; do
    install_plugin "$pin"
  done

  if [[ "$k3s" == "true" ]]; then
    log "Initializing the k3s scheduler"
    sudo dokku scheduler-k3s:initialize --server-ip 127.0.0.1
  fi
}

main "$@"
