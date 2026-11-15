#!/usr/bin/env bash
# Install the FocusLock lockdown. Run: sudo bash lockdown/linux/install.sh
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run with sudo"; exit 1; }
here=$(cd "$(dirname "$0")" && pwd)
user=${SUDO_USER:-}

echo "== removing escape-route packages"
for p in torbrowser-launcher tor firefox librewolf mullvad-browser-bin; do
  pacman -Q "$p" &>/dev/null && pacman -Rns --noconfirm "$p"
done

echo "== copying rules to /etc/focuslock-lockdown"
mkdir -p /etc/focuslock-lockdown
chattr -i /etc/focuslock-lockdown/* 2>/dev/null || true
cp "$here/../blocklist.txt" "$here/../banned-apps.txt" /etc/focuslock-lockdown/
# Keep the browser extensions you already have; block installing new ones.
if [[ -n $user ]]; then
  home=$(getent passwd "$user" | cut -d: -f6)
  ls "$home"/.config/{chromium,BraveSoftware/Brave-Browser,google-chrome}/*/Extensions 2>/dev/null \
    | grep -E '^[a-p]{32}$' | sort -u > /etc/focuslock-lockdown/allowed-extensions.txt || true
fi
chattr +i /etc/focuslock-lockdown/*

echo "== installing enforcer, pacman hook, service"
chattr -i /usr/local/sbin/focuslock-enforce /etc/pacman.d/hooks/00-focuslock-ban.hook \
          /etc/systemd/system/focuslock-lockdown.service 2>/dev/null || true
install -Dm755 "$here/focuslock-enforce" /usr/local/sbin/focuslock-enforce
install -Dm644 "$here/focuslock-ban.hook" /etc/pacman.d/hooks/00-focuslock-ban.hook
install -Dm644 "$here/focuslock-lockdown.service" /etc/systemd/system/focuslock-lockdown.service
chattr -i /etc/polkit-1/rules.d/00-focuslock-lockdown.rules 2>/dev/null || true
install -Dm644 "$here/focuslock-lockdown.rules" /etc/polkit-1/rules.d/00-focuslock-lockdown.rules
chattr +i /usr/local/sbin/focuslock-enforce /etc/pacman.d/hooks/00-focuslock-ban.hook \
          /etc/systemd/system/focuslock-lockdown.service /etc/polkit-1/rules.d/00-focuslock-lockdown.rules
systemctl daemon-reload
systemctl enable focuslock-lockdown.service
systemctl start focuslock-lockdown.service
systemctl kill focuslock-lockdown.service   # restarts it (Restart=always) with the new script

/usr/local/sbin/focuslock-enforce once
echo
echo "Done. Restart your browser, then check chrome://policy."
echo "Test: open youtube.com (blocked), run 'tor' (killed), 'resolvectl query pornhub.com'."
