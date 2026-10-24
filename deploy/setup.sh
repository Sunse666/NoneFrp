#!/usr/bin/env bash
# NoneFrp relay installer.
#
#   bash setup.sh                     交互菜单
#   bash setup.sh --domain mc.example.com             交互菜单（预填域名）
#   bash setup.sh --domain mc.example.com --auto      无人值守，跑完即退
#   bash setup.sh --prefix=/root/relay                把程序装在别处（默认 /opt/nonefrp）
#
# 每一步都可以单独重跑，也会自己判断当前状态；重复执行是安全的。

PREFIX=/opt/nonefrp
STATE=$PREFIX/.setup
UNIT=/etc/systemd/system/nonefrp.service
NGINX_LOG=/tmp/nonefrp-nginx-test.log

PORT=25565
DOMAIN=""
RELAY_HOST=""
EMAIL=""
BIN=""
FORCE=0
NO_TLS=0
AUTO=0

LAST_REASON=""
LAST_HINT=""

C_OK="\033[32m✓\033[0m"
C_NO="\033[31m✗\033[0m"
C_DOT="\033[90m·\033[0m"
C_B="\033[1m"
C_N="\033[0m"

say()  { printf '\n%s=== %s%s\n' "$C_B" "$*" "$C_N"; }
ok()   { printf '    \033[32mok\033[0m   %s\n' "$*"; }
note() { printf '    \033[36m..\033[0m   %s\n' "$*"; }
warn() { printf '    \033[33mwarn\033[0m %s\n' "$*"; }
fail() { LAST_REASON="$*"; printf '    \033[31mfail\033[0m %s\n' "$*"; }
hint() { LAST_HINT="$*"; printf '    \033[35m处理\033[0m %s\n' "$*"; }

value_of() { printf '%s' "$1" | cut -d= -f2-; }

# ---------------------------------------------------------------- state

save_state() {
  install -d -m 0755 "$PREFIX"
  {
    printf 'DOMAIN=%s\n' "$DOMAIN"
    printf 'RELAY_HOST=%s\n' "$RELAY_HOST"
    printf 'EMAIL=%s\n' "$EMAIL"
    printf 'PORT=%s\n' "$PORT"
    printf 'BIN=%s\n' "$BIN"
    printf 'NO_TLS=%s\n' "$NO_TLS"
  } > "$STATE"
  chmod 600 "$STATE"
}

load_state() {
  if [ -f "$STATE" ]; then . "$STATE"; fi
}

# ---------------------------------------------------------------- args

while [ $# -gt 0 ]; do
  case "$1" in
    --domain=*) DOMAIN="$(value_of "$1")"; shift ;;
    --domain)   DOMAIN="$2"; shift 2 ;;
    --relay=*)  RELAY_HOST="$(value_of "$1")"; shift ;;
    --relay)    RELAY_HOST="$2"; shift 2 ;;
    --email=*)  EMAIL="$(value_of "$1")"; shift ;;
    --email)    EMAIL="$2"; shift 2 ;;
    --port=*)   PORT="$(value_of "$1")"; shift ;;
    --port)     PORT="$2"; shift 2 ;;
    --bin=*)    BIN="$(value_of "$1")"; shift ;;
    --bin)      BIN="$2"; shift 2 ;;
    --force)    FORCE=1; shift ;;
    --no-tls)   NO_TLS=1; shift ;;
    --prefix=*) PREFIX="$(value_of "$1")"; STATE="$PREFIX/.setup"; shift ;;
    --prefix)   PREFIX="$2"; STATE="$PREFIX/.setup"; shift 2 ;;
    --auto)     AUTO=1; shift ;;
    -h|--help)  sed -n '2,9p' "$0"; exit 0 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 2 ;;
  esac
done

load_state

[ "$(id -u)" -eq 0 ] || { printf '需要 root 运行\n' >&2; exit 1; }

# Older releases spelled the project "nonerp" in the directory, the binary and
# the service name. Move them once so upgrading is not a manual chore.
migrate_legacy() {
  if [ "$PREFIX" != "/opt/nonefrp" ]; then return 0; fi

  if [ -d /opt/nonerp ] && [ ! -d /opt/nonefrp ]; then
    mv /opt/nonerp /opt/nonefrp
    if [ -f /opt/nonefrp/nonerp ] && [ ! -f /opt/nonefrp/nonefrp ]; then
      mv /opt/nonefrp/nonerp /opt/nonefrp/nonefrp
    fi
    printf '    ok   已把 /opt/nonerp 迁移到 /opt/nonefrp\n'
  fi

  if [ -f /etc/systemd/system/nonerp.service ]; then
    systemctl disable --now nonerp >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/nonerp.service
    systemctl daemon-reload >/dev/null 2>&1 || true
    printf '    ok   已停用旧服务名 nonerp\n'
  fi
}

migrate_legacy

# ".mc.example.com" is what people type into a domain field; the suffix must
# not start with a dot or room hosts come out as "code..mc.example.com".
DOMAIN="${DOMAIN#.}"
RELAY_HOST="${RELAY_HOST#.}"

if [ -z "$DOMAIN" ] && [ "$AUTO" -eq 1 ]; then
  printf '缺少 --domain\n' >&2; exit 2
fi

fill_derived() {
  if [ -z "$RELAY_HOST" ]; then
    BASE="$(printf '%s' "$DOMAIN" | cut -d. -f2-)"
    RELAY_HOST="relay.$BASE"
  fi
  if [ -z "$EMAIL" ]; then
    EMAIL="admin@$(printf '%s' "$RELAY_HOST" | cut -d. -f2-)"
  fi
}

find_bin() {
  if [ -n "$BIN" ] && [ -f "$BIN" ]; then return; fi
  HERE="$(cd "$(dirname "$0")" && pwd)"
  for candidate in "$HERE/nonefrp-linux-amd64" "$HERE/nonefrp" /tmp/nonefrp /root/nonefrp-linux-amd64; do
    if [ -f "$candidate" ]; then BIN="$candidate"; return; fi
  done
}

# ---------------------------------------------------------------- checks

check_env() {
  command -v systemctl >/dev/null || return 1
  command -v curl >/dev/null || return 1
  find_bin
  [ -n "$BIN" ] && [ -f "$BIN" ] || return 1
  if command -v file >/dev/null; then
    file "$BIN" | grep -q ELF || return 1
  fi
  return 0
}

check_binary() {
  [ -x "$PREFIX/nonefrp" ] || return 1
  "$PREFIX/nonefrp" version >/dev/null 2>&1
}

check_config() {
  [ -f "$PREFIX/server.yaml" ] || return 1
  [ -x "$PREFIX/nonefrp" ] || return 1
  "$PREFIX/nonefrp" server -config "$PREFIX/server.yaml" -check >/dev/null 2>&1
}

check_unit() {
  [ -f "$UNIT" ] || return 1
  systemctl is-enabled nonefrp >/dev/null 2>&1
}

check_nginx() {
  command -v nginx >/dev/null || return 1
  [ -n "$RELAY_HOST" ] || return 1
  nginx -t >/dev/null 2>&1 || return 1
  nginx -T 2>/dev/null | grep -q "server_name $RELAY_HOST"
}

check_cert() {
  [ -n "$RELAY_HOST" ] || return 1
  [ -f "/etc/letsencrypt/live/$RELAY_HOST/fullchain.pem" ] || return 1
  if command -v openssl >/dev/null; then
    openssl x509 -checkend 86400 -noout -in "/etc/letsencrypt/live/$RELAY_HOST/fullchain.pem" >/dev/null 2>&1
  fi
}

check_fw() {
  if ! command -v ufw >/dev/null; then return 0; fi
  if ! ufw status 2>/dev/null | grep -q "Status: active"; then return 0; fi
  ufw status 2>/dev/null | grep -q "$PORT/tcp"
}

check_health() {
  [ "$(curl -fsS --max-time 3 http://127.0.0.1:8080/readyz 2>/dev/null)" = "ready" ]
}

# ---------------------------------------------------------------- steps

step_env() {
  [ "$(id -u)" -eq 0 ] || { fail "不是 root"; hint "用 root 或 sudo 运行"; return 1; }
  command -v systemctl >/dev/null || { fail "没有 systemd"; hint "这台机器用的是别的 init，先手动确认"; return 1; }
  command -v curl >/dev/null || { fail "没有 curl"; hint "apt install -y curl"; return 1; }
  find_bin
  if [ -z "$BIN" ] || [ ! -f "$BIN" ]; then
    fail "找不到 Linux 二进制"
    hint "把 nonefrp-linux-amd64 和本脚本一起 scp 到 /root/，或用 --bin= 指定"
    return 1
  fi
  if command -v file >/dev/null && ! file "$BIN" | grep -q ELF; then
    fail "$BIN 不是 Linux 可执行文件"
    hint "你可能传成了 Windows 的 exe，改成 dist/nonefrp-linux-amd64"
    return 1
  fi
  [ -n "$DOMAIN" ] || { fail "还没设置域名"; hint "按 t 修改参数"; return 1; }
  fill_derived
  save_state
  ok "域名 $DOMAIN  控制面 $RELAY_HOST  端口 $PORT"
  return 0
}

step_binary() {
  install -d -m 0755 "$PREFIX"
  install -m 0755 "$BIN" "$PREFIX/nonefrp" || { fail "复制失败"; return 1; }
  VERSION="$("$PREFIX/nonefrp" version 2>&1)" || { fail "二进制跑不起来：$VERSION"; hint "重新传一次二进制"; return 1; }
  ok "$VERSION"
  return 0
}

step_config() {
  if [ -f "$PREFIX/server.yaml" ] && [ "$FORCE" -eq 0 ]; then
    if "$PREFIX/nonefrp" server -config "$PREFIX/server.yaml" -check >/dev/null 2>&1; then
      ok "已存在且有效，保留（含 token）。要重写加 --force"
      return 0
    fi
    warn "已存在但解析失败，备份后重写"
    cp "$PREFIX/server.yaml" "$PREFIX/server.yaml.bak"
  fi

  fill_derived
  cat > "$PREFIX/server.yaml" <<'YAML'
listen: "127.0.0.1:8080"
public_port: __PORT__
domain_suffix: "__DOMAIN__"
public_url: "wss://__RELAY__"
grace_period: 60s
tokens: {}
YAML
  sed -i "s/__PORT__/$PORT/; s/__DOMAIN__/$DOMAIN/; s/__RELAY__/$RELAY_HOST/" "$PREFIX/server.yaml"
  chmod 600 "$PREFIX/server.yaml"

  if ! "$PREFIX/nonefrp" server -config "$PREFIX/server.yaml" -check >/dev/null 2>&1; then
    fail "写出来的配置仍无法解析"
    "$PREFIX/nonefrp" server -config "$PREFIX/server.yaml" -check 2>&1 | sed 's/^/        /'
    return 1
  fi
  ok "已写入 $PREFIX/server.yaml"
  return 0
}

step_unit() {
  cat > "$UNIT" <<UNITFILE
[Unit]
Description=NoneFrp relay
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$PREFIX
ExecStart=$PREFIX/nonefrp server -config $PREFIX/server.yaml
Restart=always
RestartSec=2
LimitNOFILE=100000
NoNewPrivileges=true
PrivateTmp=true
KillSignal=SIGTERM
TimeoutStopSec=20

[Install]
WantedBy=multi-user.target
UNITFILE
  systemctl daemon-reload || { fail "daemon-reload 失败"; return 1; }
  systemctl enable nonefrp >/dev/null 2>&1 || { fail "开机自启设置失败"; return 1; }
  ok "已安装 $UNIT 并设为开机自启"
  return 0
}

nginx_target() {
  if [ -d /etc/nginx/sites-enabled ]; then
    NGINX_SITE=/etc/nginx/sites-available/relay.conf
    NGINX_LINK=/etc/nginx/sites-enabled/relay.conf
  elif [ -d /etc/nginx/conf.d ]; then
    NGINX_SITE=/etc/nginx/conf.d/relay.conf
    NGINX_LINK=""
  else
    return 1
  fi
  return 0
}

step_nginx() {
  if ! command -v nginx >/dev/null; then
    fail "没装 nginx"
    hint "apt install -y nginx 后重跑；如果用宝塔面板，在面板里把反向代理指到 127.0.0.1:8080"
    return 1
  fi
  if ! nginx_target; then
    fail "找不到 nginx 配置目录"
    hint "宝塔面板请用面板加站点 + 反向代理到 127.0.0.1:8080"
    return 1
  fi

  BACKUP=""
  if [ -f "$NGINX_SITE" ]; then BACKUP="$(mktemp)"; cp "$NGINX_SITE" "$BACKUP"; fi

  cat > "$NGINX_SITE" <<'NGINXCONF'
server {
    listen 80;
    listen [::]:80;
    server_name __RELAY__;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
    }
}
NGINXCONF
  sed -i "s/__RELAY__/$RELAY_HOST/" "$NGINX_SITE"
  if [ -n "$NGINX_LINK" ]; then ln -sf "$NGINX_SITE" "$NGINX_LINK"; fi

  if ! nginx -t > "$NGINX_LOG" 2>&1; then
    fail "nginx -t 没通过，已回滚"
    sed 's/^/        /' "$NGINX_LOG"
    if [ -n "$BACKUP" ]; then cp "$BACKUP" "$NGINX_SITE"; else rm -f "$NGINX_SITE"; fi
    if [ -n "$NGINX_LINK" ]; then rm -f "$NGINX_LINK"; fi
    hint "你的网站没受影响。看上面的报错，或按 l 查看日志"
    return 1
  fi
  systemctl reload nginx || { fail "nginx 重载失败"; return 1; }
  ok "已写入 $NGINX_SITE 并重载"
  return 0
}

step_cert() {
  if [ "$NO_TLS" -eq 1 ]; then
    note "按 --no-tls 跳过，房主端只能用 http://$RELAY_HOST:8080"
    return 0
  fi
  if ! command -v nginx >/dev/null; then
    warn "没有 nginx，跳过"
    return 0
  fi
  if check_nginx; then :; else
    fail "nginx 还没配好，先完成第 5 步"
    return 1
  fi
  if ! command -v certbot >/dev/null; then
    note "安装 certbot..."
    apt-get update -qq >/dev/null 2>&1
    apt-get install -y -qq certbot python3-certbot-nginx >/dev/null 2>&1 || {
      fail "certbot 安装失败"; hint "apt-get update 后 apt install certbot python3-certbot-nginx"; return 1; }
  fi
  if certbot --nginx -d "$RELAY_HOST" --non-interactive --agree-tos -m "$EMAIL" --redirect > /tmp/nonefrp-certbot.log 2>&1; then
    ok "证书已就绪，自动续期"
    return 0
  fi
  fail "证书申请失败"
  tail -5 /tmp/nonefrp-certbot.log | sed 's/^/        /'
  hint "检查：DNS 里 $RELAY_HOST 是否指向本机？80 端口对公网开放吗？"
  return 1
}

step_firewall() {
  if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow "$PORT/tcp" >/dev/null 2>&1 || { fail "ufw 放行失败"; return 1; }
    ok "ufw 已放行 $PORT/tcp"
  else
    note "ufw 未启用，跳过系统防火墙"
  fi
  note "还需要在云厂商控制台的安全组放行 TCP $PORT（脚本管不到）"
  return 0
}

step_health() {
  systemctl restart nonefrp || { fail "重启失败"; return 1; }
  for i in 1 2 3 4 5 6 7 8 9 10; do
    if check_health; then
      ok "本地 /readyz 正常"
      if [ "$NO_TLS" -eq 0 ] && command -v nginx >/dev/null && check_nginx; then
        if curl -fsS --max-time 8 "https://$RELAY_HOST/readyz" >/dev/null 2>&1; then
          ok "https://$RELAY_HOST/readyz 正常"
        else
          warn "https 还不通（证书或 DNS 未生效）"
        fi
      fi
      return 0
    fi
    sleep 1
  done
  fail "服务没起来或 /readyz 不正常"
  journalctl -u nonefrp -n 8 --no-pager | sed 's/^/        /'
  hint "多数是 server.yaml 写坏了，按 l 看日志；也可用第 3 步重写配置"
  return 1
}

step_token() {
  if ! check_health; then
    fail "中继还没跑起来"
    return 1
  fi

  OUTPUT="$("$PREFIX/nonefrp" server invite -config "$PREFIX/server.yaml" -once -note "first host" 2>&1)" || {
    fail "签发邀请码失败"
    printf '%s\n' "$OUTPUT" | sed 's/^/        /'
    return 1
  }
  CODE="$(printf '%s\n' "$OUTPUT" | sed -n 's/^invite:  *//p')"
  if [ -z "$CODE" ]; then
    fail "没能从输出里读到邀请码"
    printf '%s\n' "$OUTPUT" | sed 's/^/        /'
    return 1
  fi

  systemctl restart nonefrp
  sleep 1
  ok "已签发邀请码 $CODE（一台设备）"

  say "把这两行发给房主"
  printf '  中继地址: %s\n' "$RELAY_HOST"
  printf '  邀请码:   %s\n' "$CODE"

  say "或者直接发这条链接"
  ( cd "$PREFIX" && ./nonefrp server link -config server.yaml -token "$CODE" )
  return 0
}

# ---------------------------------------------------------------- rollback

rollback() {
  say "回滚"
  printf '这会停止并移除中继服务、nginx 站点配置。\n'
  printf '默认保留 %s（含 token 和配置）。\n' "$PREFIX"
  read -r -p '确认回滚？输入 yes 继续: ' answer
  [ "$answer" = "yes" ] || { note "已取消"; return; }

  systemctl disable --now nonefrp >/dev/null 2>&1 && ok "服务已停止并取消自启"

  if [ -f "$UNIT" ]; then rm -f "$UNIT"; systemctl daemon-reload; ok "unit 已删除"; fi

  if nginx_target 2>/dev/null; then
    if [ -n "$NGINX_LINK" ]; then rm -f "$NGINX_LINK"; fi
    if [ -f "$NGINX_SITE" ]; then rm -f "$NGINX_SITE"; ok "nginx 站点已删除"; fi
    if nginx -t >/dev/null 2>&1; then systemctl reload nginx; else warn "nginx -t 失败，未重载"; fi
  fi

  read -r -p '同时删除配置和 token（/opt/nonefrp）？输入 delete 确认: ' answer2
  if [ "$answer2" = "delete" ]; then rm -rf "$PREFIX"; ok "已删除 $PREFIX"; else note "保留 $PREFIX"; fi

  if [ -f "/etc/letsencrypt/live/$RELAY_HOST/fullchain.pem" ]; then
    read -r -p "同时删除证书 $RELAY_HOST？输入 delete 确认: " answer3
    if [ "$answer3" = "delete" ]; then certbot delete --cert-name "$RELAY_HOST" --non-interactive >/dev/null 2>&1 && ok "证书已删除"; fi
  fi

  ok "回滚完成"
}

# ---------------------------------------------------------------- menu

mark() {
  case "$1" in
    1) check_env ;;
    2) check_binary ;;
    3) check_config ;;
    4) check_unit ;;
    5) check_nginx ;;
    6) check_cert ;;
    7) check_fw ;;
    8) check_health ;;
    9) return 1 ;;
  esac
}

paint() {
  if mark "$1"; then printf '%s' "$C_OK"; else printf '%s' "$C_DOT"; fi
}

render() {
  printf '\n%sNoneFrp 中继安装器%s\n' "$C_B" "$C_N"
  if [ -n "$DOMAIN" ]; then
    printf '  玩家域名 %s   控制面 %s   端口 %s\n' "$DOMAIN" "$RELAY_HOST" "$PORT"
  else
    printf '  \033[33m还没设置域名，按 t 设置\033[0m\n'
  fi
  printf '  ----------------------------------------------------------\n'
  printf '   [%s] 1  环境检查\n' "$(paint 1)"
  printf '   [%s] 2  安装二进制\n' "$(paint 2)"
  printf '   [%s] 3  写入配置 server.yaml\n' "$(paint 3)"
  printf '   [%s] 4  安装系统服务（开机自启）\n' "$(paint 4)"
  printf '   [%s] 5  配置 nginx 反向代理\n' "$(paint 5)"
  printf '   [%s] 6  申请 TLS 证书\n' "$(paint 6)"
  printf '   [%s] 7  放行端口\n' "$(paint 7)"
  printf '   [%s] 8  启动并自检\n' "$(paint 8)"
  printf '   [%s] 9  签发第一个邀请码\n' "$(paint 9)"
  printf '  ----------------------------------------------------------\n'
  printf '   0  一键执行 1-8\n'
  printf '   t  修改域名 / 控制面 / 邮箱\n'
  printf '   l  查看服务日志\n'
  printf '   r  回滚（卸载）\n'
  printf '   q  退出\n'
}

run_step() {
  LAST_REASON=""; LAST_HINT=""
  case "$1" in
    1) say "1/9 环境检查";        step_env ;;
    2) say "2/9 安装二进制";      step_binary ;;
    3) say "3/9 写入配置";        step_config ;;
    4) say "4/9 安装系统服务";    step_unit ;;
    5) say "5/9 配置 nginx";      step_nginx ;;
    6) say "6/9 申请证书";        step_cert ;;
    7) say "7/9 放行端口";        step_firewall ;;
    8) say "8/9 启动并自检";      step_health ;;
    9) say "9/9 签发邀请码";      step_token ;;
  esac
}

run_all() {
  FAILED=0
  for n in 1 2 3 4 5 6 7 8; do
    run_step "$n" || FAILED=1
  done
  return "$FAILED"
}

edit_params() {
  printf '\n当前域名: %s\n' "$DOMAIN"
  read -r -p '玩家域名后缀（例如 mc.example.com，回车保持不变）: ' input
  if [ -n "$input" ]; then DOMAIN="$input"; RELAY_HOST=""; fi
  read -r -p "控制面域名（回车用默认 relay.$(printf '%s' "$DOMAIN" | cut -d. -f2-)）: " input
  if [ -n "$input" ]; then RELAY_HOST="$input"; fi
  fill_derived
  read -r -p "证书邮箱（回车用 $EMAIL）: " input
  if [ -n "$input" ]; then EMAIL="$input"; fi
  read -r -p "玩家端口（回车用 $PORT）: " input
  if [ -n "$input" ]; then PORT="$input"; fi
  save_state
  ok "已保存"
}

if [ "$AUTO" -eq 1 ]; then
  fill_derived
  save_state
  FAILED=0
  for n in 1 2 3 4 5 6 7 8; do
    run_step "$n" || FAILED=1
  done
  say "完成"
  if [ "$FAILED" -eq 0 ]; then
    printf '  下一步：签发 token\n'
    printf '    cd %s && ./nonefrp server token new -config server.yaml -note "我的服"\n' "$PREFIX"
    printf '    systemctl restart nonefrp\n'
    printf '    ./nonefrp server link -config server.yaml -token <token>\n'
    exit 0
  fi
  printf '  有步骤失败，重跑 bash %s 用菜单逐步处理\n' "$0"
  exit 1
fi

if [ -z "$DOMAIN" ]; then
  printf '\n先设置域名（玩家地址形如 <房间码>.mc.example.com）\n'
  read -r -p '玩家域名后缀: ' DOMAIN
  fill_derived
  save_state
fi

while true; do
  render
  read -r -p '选择: ' choice
  case "$choice" in
    0) run_all ;;
    1|2|3|4|5|6|7|8|9) run_step "$choice" ;;
    t) edit_params ;;
    l) systemctl status nonefrp --no-pager 2>/dev/null | head -12; printf '\n'; journalctl -u nonefrp -n 20 --no-pager 2>/dev/null ;;
    r) rollback ;;
    q|Q) printf '已退出\n'; exit 0 ;;
    *) printf '无效选择\n' ;;
  esac
  if [ -n "$LAST_REASON" ]; then
    printf '\n    \033[31m上一步失败\033[0m %s\n' "$LAST_REASON"
    if [ -n "$LAST_HINT" ]; then printf '    \033[35m建议\033[0m   %s\n' "$LAST_HINT"; fi
  fi
done
