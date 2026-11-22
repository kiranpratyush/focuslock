#!/usr/bin/env bash
# Remove the lockdown (needs root, on purpose).
set -u
[[ $EUID -eq 0 ]] || { echo "run with sudo"; exit 1; }
chattr -i /etc/systemd/system/focuslock-lockdown.service /etc/polkit-1/rules.d/00-focuslock-lockdown.rules 2>/dev/null
rm -f /etc/polkit-1/rules.d/00-focuslock-lockdown.rules
systemctl disable focuslock-lockdown.service
systemctl kill focuslock-lockdown.service
f=(/usr/local/sbin/focuslock-enforce /etc/pacman.d/hooks/00-focuslock-ban.hook /etc/pacman.d/hooks/00-focuslock-games.hook
   /etc/systemd/system/focuslock-lockdown.service /etc/NetworkManager/conf.d/99-focuslock-dns.conf
   /etc/{chromium,brave}/policies/managed/focuslock.json /etc/opt/{chrome,edge}/policies/managed/focuslock.json
   /etc/focuslock-lockdown/* /etc/hosts)
chattr -i "${f[@]}" 2>/dev/null
chattr -i /etc/NetworkManager/conf.d/20-omarchy-dns.conf 2>/dev/null
rm -f "${f[@]:0:9}"; rm -rf /etc/focuslock-lockdown /var/lib/focuslock-lockdown /run/focuslock-lockdown /usr/local/bin/focuslock-x
sed -i '/^# >>> focuslock lockdown >>>$/,/^# <<< focuslock lockdown <<<$/d' /etc/hosts
nft delete table inet focuslock 2>/dev/null
systemctl daemon-reload; nmcli general reload dns-full
echo "Lockdown removed."
