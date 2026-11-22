#!/usr/bin/env bash
# Checks which programs the enforcer would kill. Kills nothing, needs no root.
here=$(cd "$(dirname "$0")" && pwd)
SRC=$here/..
# Only the matching functions, not the enforcement loop.
eval "$(sed -n '/^list() {/,/^# Runs every 5/p' "$here/focuslock-enforce")"
load_banned
fails=0
t() { # t ban|ok <process name> <executable>
  local got=ok; is_banned "$2" "$3" && got=ban
  [[ $got == "$1" ]] || { echo "FAIL want=$1 got=$got: $2 | $3"; fails=$((fails + 1)); }
}
(( ${#names[@]} > 100 )) || { echo "FAIL the lists didn't load"; fails=1; }
W=/usr/lib/wine/x86_64-unix/wine-preloader
t ban re4.exe "$W"
t ban DOOMEternalx64v "$W"
t ban whatever.exe "$W"
t ban wineserver /usr/bin/wineserver
t ban lutris /usr/bin/python3
t ban steam /usr/bin/bash
t ban umu-run /usr/bin/python3
t ban moonlight /usr/bin/moonlight
t ban x /home/u/.local/share/Steam/compatibilitytools.d/GE/files/bin/foo
t ban x "/mnt/SteamApps/common/Some Game/game"
t ban firefox /usr/lib/firefox/firefox
t ban tor /usr/bin/tor
t ban torbrowser-laun /usr/bin/python3
t ban tor "/usr/bin/tor (deleted)"
t ok claude.exe /home/u/x/claude.exe   # some Linux tools are named .exe
t ok bun.exe /home/u/x/bun.exe
t ok brave /opt/brave-bin/brave
t ok bash /usr/bin/bash
t ok dolphin /usr/bin/dolphin          # KDE's file manager, not the emulator
t ok java /usr/bin/java
t ok nox /usr/bin/python3              # a Python tool
t ok guessing_game /home/u/rust/guessing_game/target/debug/guessing_game
t ok kworker/0:1 ""
t ok focuslock /usr/bin/python3
(( fails == 0 )) && echo "lockdown matching: all passed"
exit $(( fails > 0 ))
