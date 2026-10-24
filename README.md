# NoneFrp

让开在家里 Windows 电脑上的 Minecraft 服务器，被外部的设备直连进来。


```
玩家 ──TCP──▶ 中继服务器（Linux, 公网 IP）  ◀──WSS 出站── 房主
                    │                                        │
                    └─ 按 MC 握手里的域名分流 ───────────────┴──▶ 127.0.0.1:25565
```

---

## 目录

- [功能](#功能)
- [启动](#启动)
- [部署中继服务器](#部署中继服务器)
- [房主端](#房主端)
- [邀请码](#邀请码)
- [玩家地址与直连](#玩家地址与直连)
- [日常运维](#日常运维)
- [排障](#排障)
- [从源码构建](#从源码构建)
- [项目结构](#项目结构)

---

## 功能

| 能力 | 说明 |
|---|---|
| 单端口多房间 | 中继只监听 25565，按 MC 握手包里的域名把玩家分到不同房间，不用开一堆端口 |
| 房间码 | 玩家地址形如 `a2b3c4.mc.example.com` |
| 邀请码 | 一次性邀请码或永久码 |
| 直连分流 | 界面会列出局域网、公网 IPv6、社团网络（Tailscale 等）三类地址，能直连就不走中继，延迟更低、不耗流量 |
| 房主零配置 | 启动.exe → 网页里粘贴邀请码 → 保存 |
| 可诊断 | 网页显示连接状态、在线人数、流量、**本地服务器是否在监听**；实时日志 |

不支持：基岩版（UDP 19132）、语音模组等 UDP 服务、1.6 及更早的客户端。

## 启动

**服务器**

```powershell
scp -r .\dist\nonefrp-bundle root@<服务器IP>:/root/nonefrp
ssh root@<服务器IP> 'bash /root/nonefrp/setup.sh'
```

或是手动上传后执行

```bash
bash /root/nonefrp/setup.sh
```

菜单里按 **0** 跑完 1-8 步，再按 **9** 生成第一个邀请码。

**房主**

双击 `NoneFrp-windows-amd64.exe`，浏览器弹出后填：

```
中继地址   relay.example.com
邀请码     JOIN-XXXXXXXX
```

点「测试连接」确认，再点「保存并重连」。

**玩家**

多人游戏 → 添加服务器 → `a2b3c4.mc.example.com:25565`。

---

## 部署中继服务器

### 设备需求

- 一台有**公网 IPv4** 的 Linux（Ubuntu 22.04 / Debian 12 等）,1H1G
- 一个域名，两条 A 记录：

  | 记录 | 指向 | 说明 |
  |---|---|---|
  | `relay.example.com` | 服务器 IP | 房主连的控制面（TLS） |
  | `*.mc.example.com` | 服务器 IP | 玩家入口，**必须 DNS only，不能挂 CDN** |

- 端口放行：**80、443、25565**（云厂商安全组 + 系统防火墙都要）

> 挂 Cloudflare 的话，`*.mc` 那条一定要是灰色云朵。橙云不代理 25565，玩家会连到 Cloudflare 的 IP 上然后失败。

### 一键安装

```bash
scp -r nonefrp-bundle root@<服务器IP>:/root/nonefrp
ssh root@<服务器IP>
bash /root/nonefrp/setup.sh
```

或者上传文件后

```bash
bash /root/nonefrp/setup.sh

```

脚本会渲染一个菜单，每一步自己检测当前状态：

```
 [✓] 1  环境检查
 [✓] 2  安装二进制
 [✓] 3  写入配置 server.yaml
 [✓] 4  安装系统服务（开机自启）
 [✓] 5  配置 nginx 反向代理
 [✓] 6  申请 TLS 证书
 [✓] 7  放行端口
 [✓] 8  启动并自检
 [ ] 9  签发第一个邀请码

   0  一键执行 1-8
   t  修改域名 / 控制面 / 邮箱
   l  查看服务日志
   r  回滚（卸载）
   q  退出
```

失败的那一步会打印**原因**和**该怎么处理**。nginx 配置写坏了会自动回滚，不会影响同一台机器上已有的网站。

无人值守：

```bash
bash setup.sh --domain mc.example.com --auto
```

可选参数：`--relay=`、`--email=`、`--port=`、`--prefix=`、`--force`、`--no-tls`。

### 手动安装

```bash
install -d /opt/nonefrp
install -m 0755 nonefrp-linux-amd64 /opt/nonefrp/nonefrp

cat > /opt/nonefrp/server.yaml <<'YAML'
listen: "127.0.0.1:8080"          # 只给反代用，不直接对外
public_port: 25565                # 玩家连这个
domain_suffix: "mc.example.com"   # 玩家地址 = <房间码>.mc.example.com
public_url: "wss://relay.example.com"
grace_period: 60s
tokens: {}
YAML
chmod 600 /opt/nonefrp/server.yaml

# 服务：内容见 deploy/nonefrp.service
systemctl daemon-reload && systemctl enable --now nonefrp

# nginx：内容见 deploy/nginx-relay.conf，注意 Upgrade / Connection 两行
certbot --nginx -d relay.example.com
ufw allow 25565/tcp
```

### 配置文件字段

| 字段 | 默认 | 说明 |
|---|---|---|
| `listen` | `127.0.0.1:8080` | 控制面监听地址，放在反代后面就不要改 |
| `public_port` | `25565` | 玩家连接的端口 |
| `domain_suffix` | — | 玩家域名的后缀 |
| `public_url` | — | 房主连接用的地址，`server link` 用它生成邀请链接 |
| `grace_period` | `60s` | 房主断线后房间保留多久 |
| `limits.max_players_per_room` | `10` | 每个房间最多几个玩家（服务器列表 ping 不占名额） |
| `limits.max_streams_per_room` | `64` | 每个房间最多几条并发流 |
| `limits.conn_rate_per_ip` | `20` | 每个 IP 每秒最多新建几条连接 |
| `tokens` | — | 邀请码表，见下 |

---

## 房主端

### 首次

把 `NoneFrp-windows-amd64.exe` 发给房主，双击。浏览器会打开一个本地页面，填中继地址和邀请码即可。

> 无窗口版：双击不会弹黑框，日志写在 exe 旁边的 `nonefrp.log`。需要现场看日志时用 `NoneFrp-windows-amd64-console.exe`，行为和它完全一样，只是多一个控制台窗口。

配置写在 **exe 同目录**的 `client.yaml`，日志写在同目录的 `nonefrp.log`。

命令行等价写法：

```powershell
.\NoneFrp-windows-amd64.exe client -join "nonefrp://join?relay=...&token=..."
```

`-join` 连配置文件都不需要预先存在。

### 再次

双击启动 exe

### 配置文件字段

```yaml
relay:
  - name: relay-1
    url: wss://relay.example.com
local:
  port: 25565            # 本地 Minecraft 服务器端口
  allow_lan: false
  proxy_protocol: false  # 开启后本地服务端能看到真实玩家 IP
room:
  code: auto             # auto = 由中继分配
token: JOIN-XXXXXXXX
ui:
  enabled: true
  open_browser: true
```

命令行参数：`-config`、`-relay`、`-token`、`-join`、`-debug`、`-check`。

### 界面

- **房间地址**：发给玩家的地址
- **在线玩家 / 上行 / 下行**：实时刷新
- **本地服务器**：每 5 秒探测一次本地端口。显示「未监听」说明 Minecraft 服务端没开或端口不一致，页面会直接给出红色警告
- **直连地址**：局域网 IPv4、公网 IPv6，能直连的人可以绕过中继
- **设置**：改中继地址、邀请码、端口、房间码，保存后自动重连

界面只监听 `127.0.0.1`，路径里带随机串，并校验 `Host` 头，所以外网访问不到

---

## 邀请码

在服务器上签发：

```bash
cd /opt/nonefrp

./nonefrp server invite --once --note "frp1"       # 一台设备
./nonefrp server invite --devices 5 --days 30     # 五台设备，30 天
./nonefrp server invite --note "frp2"             # 永久、不限设备

# 让中继读到新码。SIGHUP 不会断开在线房主，比 restart 好
kill -HUP $(systemctl show -p MainPID --value nonefrp)
```

- 输出形如 `JOIN-3X8P2R9T`，把它和 `relay.example.com` 一起发给房主
- 「一次性」指**一台设备**：同一台机器断线重连、重启电脑都不受影响
- 使用设备设备记录在 `/opt/nonefrp/state.json`

查看：

```bash
./nonefrp server token ls -config server.yaml
# JOIN-3X8P2R9T  1/1 devices, never expires
# NFRP-LEGACY22  unlimited devices, never expires
```

撤销：删掉 `server.yaml` 里对应的那一行，重启服务。

> 邀请码等于权限。它不能顶掉别人的房间（房间码已绑定设备），但拿到的人可以在你这台中继上开房间、消耗带宽。

想让某个房主的地址固定下来：

```bash
./nonefrp server link -config server.yaml -token JOIN-3X8P2R9T --room mcxiao
```

然后让他在界面「房间码」里填 `mcxiao`，地址就永远是 `mcxiao.mc.example.com`。

---

## 玩家地址与直连

玩家填：`<房间码>.mc.example.com:25565`

同一局域网、或双方都有公网 IPv6 时，可以用界面上的**直连地址**，不经过中继：

| 地址 | 谁能用 |
|---|---|
| `192.168.x.x:25565` | 同一个路由器下的人 |
| `[2409:...]:25565` | 有公网 IPv6 的人（多数移动/联通宽带和手机流量） |

直连的额外好处：本地服务端天然能看到真实玩家 IP，也不需要开 `proxy_protocol`。

## 玩家端（可选）

以上都不需要玩家装东西。但如果想让玩家只填一次 `localhost`、并且自动走最快的路，可以发一个小工具：

```powershell
nonefrp join -relay relay.example.com -room a2b3c4
```

它做三件事：

1. 向中继查这个房间现在能从哪几个地址连
2. 在本机监听 `127.0.0.1:25565`
3. 每条连接**先试房主的直连地址**（IPv6 / 公网 IPv4），连不上才走中继

玩家在 Minecraft 里填 `localhost` 就行，不用记房间域名，房主换网络也不受影响。

包里带了 `join-room.cmd`：改一行 `RELAY=`（**整行替换**成中继域名，比如 `relay.example.com`，别把示例里的 `.com` 留下），双击，粘贴房主发来的**整条房间地址**就行（`7fcrfu.mc.example.com:25565`），它会自己取出房间码。

本机 25565 被占用（比如房主自己在同一台机器上开服）时会自动换下一个可用端口，并在窗口里告诉你该填 `localhost:25566`。要配合 `NoneFrp-windows-amd64-console.exe` 使用（无窗口版看不到提示）。

> 它不消耗邀请码、不需要 token：房间的直连地址本来就是公开信息。

## 社团网络（可选）

如果玩的人固定、都愿意装一次客户端，可以再开一套点对点网络：**流量完全不经过中继，延迟最低**。界面上会多出一张「社团网络地址」卡片。

部署方法见 [deploy/HEADSCALE.md](deploy/HEADSCALE.md)。要点：

- 中继扛不住或延迟不满意时再上，它不是必需品
- 装了某套网络客户端的人用 `100.x` 地址，没装的人继续用房间地址
- 两套地址同时有效，房主一次发两条即可

---

## 日常运维

```bash
systemctl status nonefrp           # 状态
systemctl restart nonefrp          # 改完配置
journalctl -u nonefrp -f           # 实时日志
curl -s https://relay.example.com/readyz    # 健康检查，期望 ready
kill -HUP $(systemctl show -p MainPID --value nonefrp)   # 重新读邀请码，不断线
```

加了邀请码之后**不需要重启**，发一个 SIGHUP 就行：中继重新读 `server.yaml` 的 `tokens:`，在线房主不受影响。只有改端口、域名、监听地址这类字段才需要 `systemctl restart`。

### 指标

```bash
curl -s -H "Authorization: Bearer $TOKEN" https://relay.example.com/metrics
```

`/metrics` 是 Prometheus 文本格式，带凭证才能读（因为它和反代共用同一个 hostname）。输出房间数、在线玩家、数据会话、注册总数、上下行字节、按原因分类的拒绝次数。

健康检查命令行工具：

```bash
./nonefrp server -config server.yaml -check
```

升级：

```bash
install -m 0755 nonefrp.new /opt/nonefrp/nonefrp
systemctl restart nonefrp
```

房主端不需要跟着升级：协议向后兼容，中继可随时重启，房主端会自动重连。

每月建议看一眼：`df -h /`、`certbot certificates`、云厂商的流量用量。

---

## 排障

| 现象 | 原因 |
|---|---|
| 玩家看到「房间不存在」 | 地址里的房间码和房主当前的不一致 |
| 玩家看到「房主暂时不在线」 | 房主端断线，房间在宽限期（默认 60 秒） |
| 玩家看到「房间已满」 | 达到 `max_players_per_room` |
| 房主界面「本地服务器 未监听」 | Minecraft 服务端没开，或端口和设置里不一致 |
| 房主界面未连接 + 401 | 邀请码不存在或已被撤销 |
| 房主界面未连接 + 连接超时 | 中继地址写错、中继没跑、或 443 没放行 |
| 房主一直 `relay attempt failed` | nginx 少了两行 `Upgrade` / `Connection` |
| `https://relay.../readyz` 返回 502 | 中继没起来，`systemctl status nonefrp`；多半是 `server.yaml` 写坏了 |
| 玩家看到「无法连接到服务器」，房主却显示已连接 | 开了「真实 IP 透传」（PROXY 协议）但服务端是原版 Paper——原版会直接断开。到设置里取消勾选 |
| 玩家能连但一直卡登录 | 服务器列表能刷新说明隧道通，问题在 Paper 侧 |
| 房间码每次重启都变 | 中继低于 0.3.4；升级即可，或在设置里把房间码填死 |
| 「一次性邀请码用过了」 | 换了机器或删了 `device_id`；把该码的 `max_devices` 改成 2 救急 |
| 双击无窗口版什么都没发生 | 看 exe 旁边的 `nonefrp.log`；启动就失败的话看 `startup-error.log` |

服务器日志在 `journalctl`，房主日志在 exe 旁边的 `nonefrp.log`。加 `-debug` 能看到每条流的字节数和每个拒绝原因。

---

## 从源码构建

需要 Go 1.26+。

```powershell
.\build.ps1 -Version 0.3.9
```

产物：

| 文件 | 用途 |
|---|---|
| `dist/NoneFrp-windows-amd64.exe` | 房主端（无窗口，双击用） |
| `dist/NoneFrp-windows-amd64-console.exe` | 房主端（带控制台，调试用） |
| `dist/nonefrp-linux-amd64` | 中继（amd64） |
| `dist/nonefrp-linux-arm64` | 中继（树莓派等） |
| `dist/nonefrp-darwin-arm64` | 房主端（Mac） |
| `dist/nonefrp-bundle/` | 上传到服务器的完整包（二进制文件 + 脚本 + 文档） |

测试：

```bash
gofmt -l . && go vet ./... && go test ./...
```

## 项目结构

```
cmd/nonefrp/          单入口：server / client / server token|link|invite
internal/
  protocol/          控制面消息、流 preface
  room/              房间状态机（active / grace / closed）
  channel/           数据通道：yamux over WebSocket
  proxy/             双向转发（半关闭、空闲超时）
  mcproto/           MC 握手解析、状态响应、PROXY 协议
  server/            中继：鉴权、路由、限流、核销
  client/            房主端：注册、重连、转发、探测
  supervisor/        配置变更时重启隧道
  ui/                本地网页界面
  config/            配置加载与保存（含凭证策略）
  netinfo/           直连地址枚举
  logging/           日志（stderr + 轮转文件）
deploy/              部署脚本与运维文档
```

---

## 安全说明

- 控制面全程 TLS（`wss://`），中继不解析 MC 登录后的加密流量，只读握手包
- 玩家流量在公网中继上是明文的，和直连 Minecraft 服务器一样；需要端到端加密请自行在前面套一层
- 网页界面只绑回环、带随机路径、校验 Host，防止本机其它网页读取邀请码
- `proxy_protocol` 默认关闭：开启后本地服务端会收到 PROXY 头，原版服务端会解析失败
