# 社团网络（Headscale + Tailscale）

给自己人用的第二套连接方式：**点对点直连，不消耗中继流量，延迟更低**。

中继地址始终有效，这套东西是加菜，不是替换。没加入社团网络的人继续用中继，什么都不影响。

```
房主 ──P2P 直连──► 社员      打洞成功时，流量完全不经过你的 VPS
  └──DERP 回落──► 打洞失败时才兜底（可以用自建 derper，也可以只用官方）
```

---

## 0. 先量一下成功率

装好之后，在房主机器上对某个社员执行：

```bash
tailscale ping 100.x.x.x
```

- `pong from ... via 1.2.3.4:41641` —— **直连**，这就是你要的
- `pong from ... via DERP(tok)` —— 回落了，流量走中继

如果大多数人都是 direct，这套方案值得推。如果大量回落 DERP，先把 IPv6 打通——打洞优先走 IPv6，而 IPv6 没有 NAT 问题。

---

## 1. 前提

| 需要 | 说明 |
|---|---|
| 域名 | `headscale.example.com`，A 记录指向 VPS |
| 端口 | 443（复用现有 nginx） |
| 磁盘/内存 | 一个二进制 + 一个 sqlite，几十 MB |

## 2. 安装 headscale

```bash
# Debian/Ubuntu
curl -fsSL https://github.com/juanfont/headscale/releases/latest/download/headscale_linux_amd64 -o /usr/local/bin/headscale
chmod +x /usr/local/bin/headscale

install -d /etc/headscale /var/lib/headscale
headscale generate private-key > /etc/headscale/private.key
```

## 3. 配置

```yaml
# /etc/headscale/config.yaml
server_url: https://headscale.example.com
listen_addr: 127.0.0.1:8081

# 地址池按需改，默认就是 100.64.0.0/10
ip_prefixes:
  - 100.64.0.0/10
  - fd7a:115c:a1e0::/48

# 让社员不用记 IP：在 MC 里填 mc-host.club:25565
magic_dns: true
dns:
  base_domain: club.internal
  nameservers.global:
    - 1.1.1.1

# 先只用官方 DERP 兜底；要完全自控再自建 derper
derp:
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  auto_update_enabled: true
```

启动：

```bash
headscale serve &
# 或者写个 systemd unit，和 nonefrp 一样的套路
```

## 4. 443 与现有 nginx 共存

中继已经占了 443（`relay.example.com`）。headscale 不能自己监听 443，交给 nginx：

```nginx
server {
    listen 443 ssl http2;
    server_name headscale.example.com;

    ssl_certificate     /etc/letsencrypt/live/headscale.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/headscale.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;

        # 漏了这条会表现为「登录卡住」
        proxy_buffering off;
    }
}
```

证书：

```bash
certbot --nginx -d headscale.example.com
nginx -t && systemctl reload nginx
```

## 5. 发钥匙

```bash
headscale users create club
headscale preauthkeys create --user club --reusable --expiration 720h
# tskey-auth-xxxxxxxxxxxxxxxx
```

`--reusable` 意味着一把钥匙可以给整个社团用。**包发到哪，钥匙就到哪**，泄了就用
`headscale preauthkeys list` 查、`expire` 掉，重新发一把。

## 6. 房主（服务端那台 Windows）

```powershell
winget install tailscale.tailscale          # 或者官网下载 msi
tailscale up --login-server https://headscale.example.com --authkey tskey-auth-xxxx
tailscale ip -4                              # 100.x.x.x
tailscale status                             # 看自己叫什么
```

两个坑：

**① Windows 防火墙**。Tailscale 网卡通常被识别成「公用网络」，旧的 `Minecraft 25565` 规则可能只对「专用网络」生效。要么把规则改成所有配置文件，要么单独加一条。

**② Paper 绑定地址**。必须监听 `0.0.0.0` 或 `::`，不能只绑 `127.0.0.1`。

界面上的「社团网络地址」卡片会显示这个 `100.x`，装了客户端才有，没装就自动隐藏。

## 7. 社员

```powershell
winget install tailscale.tailscale
tailscale up --login-server https://headscale.example.com --authkey tskey-auth-xxxx
```

然后在 Minecraft 里填：

```
mc-host.club.internal:25565      ← MagicDNS，最省事
100.x.x.x:25565                  ← 房主发给你的那个
```

## 8. 验证清单

```bash
tailscale status            # 两个节点都在，状态是 direct 还是 relay
tailscale ping mc-host      # 直连会显示 via <ip>:41641
tailscale netcheck          # 看自己的 NAT 类型
```

房主这边确认服务真的在 Tailscale 网卡上听着：

```powershell
Get-NetTCPConnection -LocalPort 25565 -State Listen | Select-Object LocalAddress
# 期望看到 0.0.0.0 或 ::
```

## 9. 常见问题

| 现象 | 原因 |
|---|---|
| `tailscale up` 卡住 | nginx 少了 `proxy_buffering off`，或 443 没通 |
| 两个节点一直 relay | 运营商对称 NAT；先确认 IPv6 可用再谈打洞 |
| 社员能 ping 通但 MC 连不上 | 房主防火墙 / Paper 只绑了 127.0.0.1 |
| MagicDNS 名字解析不了 | `magic_dns: true` 没开，或客户端没重启 |
| 钥匙过期 | `headscale preauthkeys create` 重新发，客户端重跑一次 `up` |

## 10. 什么时候不需要它

- 社员不愿意装客户端 → 直接用房间地址（中继）
- 房主和社员都有公网 IPv6 → 界面上那个 IPv6 直连地址就是零安装的 P2P
- 只是两三个人偶尔玩 → 中继的延迟其实可以接受
