#!/usr/bin/env bash
set -euo pipefail

artifact_dir=${1:?artifact directory is required}
os_id=$(. /etc/os-release && printf '%s' "$ID")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

case "$os_id" in
  fedora)
    package=$(find "$artifact_dir" -maxdepth 1 -type f -name '*-linux-amd64.rpm' -print -quit)
    test -n "$package"
    dnf install -y xorg-x11-server-Xvfb dbus-x11
    dnf install -y "$package"
    ;;
  ubuntu|debian)
    package=$(find "$artifact_dir" -maxdepth 1 -type f -name '*-linux-amd64.deb' -print -quit)
    test -n "$package"
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y dbus-x11 xvfb "$package"
    ;;
  *)
    echo "unsupported GUI smoke-test image: $os_id" >&2
    exit 1
    ;;
esac

export HOME="$work/home"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_DATA_HOME="$HOME/.local/share"
export XDG_STATE_HOME="$HOME/.local/state"
mkdir -p "$HOME"
set +e
timeout --signal=TERM --kill-after=5s 20s xvfb-run -a dbus-run-session -- /usr/bin/agent-marketplace-gui
status=$?
set -e
if [[ $status -ne 124 ]]; then
  echo "GUI exited before the launch smoke-test deadline (status $status)" >&2
  exit 1
fi
