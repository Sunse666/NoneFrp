#!/usr/bin/env bash
# Read only preflight for a server that already runs a website.
# Run it as root and paste the output back; it changes nothing.
#
#   scp deploy/preflight.sh root@HOST:/tmp/
#   ssh root@HOST 'bash /tmp/preflight.sh'

set -u

section() { printf '\n===== %s =====\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

section "system"
uname -srm
[ -r /etc/os-release ] && . /etc/os-release && echo "distro: ${PRETTY_NAME:-unknown}"
echo "init: $(ps -p 1 -o comm= 2>/dev/null)"
echo "cpu: $(nproc) cores"
free -h 2>/dev/null | awk 'NR<=2'
df -h / 2>/dev/null | awk 'NR<=2'

section "addresses"
if have ip; then
  ip -4 addr show scope global | awk '/inet /{print "ipv4: "$2}'
  ip -6 addr show scope global | awk '/inet6 /{print "ipv6: "$2}'
fi
echo "public ipv4 (via ifconfig.co): $(curl -4 -s --max-time 5 ifconfig.co 2>/dev/null || echo unavailable)"
echo "public ipv6 (via ifconfig.co): $(curl -6 -s --max-time 5 ifconfig.co 2>/dev/null || echo unavailable)"

section "listeners on the ports we care about"
if have ss; then
  ss -lntp 2>/dev/null | awk 'NR==1 || /:(80|443|25565|8080)\>/'
else
  netstat -lntp 2>/dev/null | awk 'NR<=2 || /:(80|443|25565|8080)\>/'
fi

section "web server"
for unit in nginx caddy apache2 httpd; do
  if systemctl list-unit-files 2>/dev/null | grep -q "^${unit}\.service"; then
    state=$(systemctl is-active "${unit}" 2>/dev/null)
    version=$(command -v "${unit}" >/dev/null 2>&1 && "${unit}" -v 2>&1 | head -1)
    echo "${unit}: ${state} ${version}"
  fi
done
[ -d /etc/nginx/sites-enabled ] && echo "nginx sites: $(ls /etc/nginx/sites-enabled | tr '\n' ' ')"
[ -f /etc/caddy/Caddyfile ] && echo "caddyfile: present"

section "firewall"
have ufw && ufw status 2>/dev/null | head -6
have firewall-cmd && firewall-cmd --state 2>/dev/null && firewall-cmd --list-ports 2>/dev/null
iptables -S 2>/dev/null | grep -c . | sed 's/^/iptables rules: /'

section "existing relay?"
ls -la /opt/nonefrp 2>/dev/null || echo "no /opt/nonefrp yet"
systemctl is-active nonefrp 2>/dev/null || true

section "bandwidth hint"
echo "check your plan's bandwidth: 8 Mbps carries roughly 10 minecraft players."
echo "a 1 TB monthly allowance is roughly 4 players online around the clock."

echo
echo "done."
