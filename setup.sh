#!/usr/bin/env bash
# One command to build and install FocusLock on Arch / Omarchy.
# Installs whatever is missing (Go, Qt 6, layer-shell-qt, build tools),
# then builds and installs the package so it shows up in the app launcher.
#
#   bash setup.sh                 from a clone
#   curl -fsSL https://raw.githubusercontent.com/kiranpratyush/focuslock/main/setup.sh | bash
set -euo pipefail
REPO_URL=https://github.com/kiranpratyush/focuslock.git
GO_MIN=1.24

command -v pacman >/dev/null || { echo "This script is for Arch-based systems (pacman)."; exit 1; }

# Run from a clone, or clone into ~/focuslock.
here=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)
if [[ -n $here && -f $here/go.mod && -d $here/app ]]; then
  repo=$here
else
  command -v git >/dev/null || sudo pacman -S --needed --noconfirm git
  repo=$HOME/focuslock
  [[ -d $repo/.git ]] || git clone "$REPO_URL" "$repo"
fi
echo "== FocusLock source: $repo"

# Packages. --needed skips what's already installed.
pkgs=(base-devel qt6-base qt6-declarative qt6-wayland layer-shell-qt)
missing=()
for p in "${pkgs[@]}"; do pacman -Qq "$p" &>/dev/null || pacman -Qqg "$p" &>/dev/null || missing+=("$p"); done

# Go: any Go >= 1.24 on PATH is fine (pacman, mise, ...).
go_ok() {
  command -v go >/dev/null || return 1
  local v; v=$(go env GOVERSION 2>/dev/null); v=${v#go}
  [[ $(printf '%s\n%s\n' "$GO_MIN" "$v" | sort -V | head -1) == "$GO_MIN" ]]
}
go_ok || missing+=(go)

if ((${#missing[@]})); then
  echo "== installing: ${missing[*]}"
  sudo pacman -S --needed --noconfirm "${missing[@]}"
  hash -r
fi
go_ok || { echo "Go $GO_MIN+ is needed; found: $(go version 2>/dev/null || echo none)"; exit 1; }

echo "== building and installing"
cd "$repo/app"
bin/install --noconfirm

echo
echo "Done. Open FocusLock from the launcher (Super + Space), or run: focuslock start"
