# NoneFrp

让开在家里 Windows 电脑上的 Minecraft 服务器，被外网的朋友直接连进来。

房主双击一个 exe，在网页里填两样东西；朋友在多人游戏里填一个地址。不需要公网 IP，不需要改路由器，不需要装 Java 以外的任何运行环境。

```
玩家 ──TCP──▶ 中继服务器（Linux, 公网 IP）  ◀──WSS 出站── 房主（Windows, NAT 后面）
                    │                                        │
                    └─ 按 MC 握手里的域名分流 ───────────────┴──▶ 127.0.0.1:25565
```

房主主动连出去，中继从不主动连房主，所以房主在哪都能用：家里宽带、校园网、公司网、手机热点。

---

## 目录

- [它做什么](#它做什么)
- [五分钟跑通](#五分钟跑通)
- [部署中继服务器](#部署中继服务器)
- [房主端](#房主端)
- [邀请码](#邀请码)
- [玩家地址与直连](#玩家地址与直连)
- [日常运维](#日常运维)
- [排障](#排障)
- [从源码构建](#从源码构建)
- [项目结构](#项目结构)

---

## 它做什么

| 能力 | 说明 |
|---|---|
| 单端口多房间 | 中继只监听 25565，按 MC 握手包里的域名把玩家分到不同房间，不用开一堆端口 |
| 房间码即域名 | 玩家地址形如 `a2b3c4.mc.example.com`，比 IP:端口 好记也好发 |
| 邀请制 | 一次性邀请码（一台设备）或永久码；用掉多少台设备都记录在案 |
| 直连分流 | 界面会列出局域网地址和公网 IPv6 地址，能直连就不走中继，延迟更低、不耗流量 |
| 房主零配置 | exe 双击 → 网页里粘贴邀请码 → 完成。配置存在 exe 旁边，换机器拷走即可 |
| 可诊断 | 网页显示连接状态、在线人数、流量、**本地服务器是否在监听**；日志同时写文件 |

不支持：基岩版（UDP 19132）、语音模组等 UDP 服务、1.6 及更早的客户端。

## 五分钟跑通

**服务器（一次性）**

```powershell
scp -r .\dist\nonerp-bundle root@<服务器IP>:/root/nonerp
ssh root@<服务器IP> 'bash /root/nonerp/setup.sh'
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

### 需要什么

- 一台有**公网 IPv4** 的 Linux（Ubuntu 22.04 / Debian 12 都行），1 核 1G 就够
- 一个域名，两条 A 记录：

  | 记录 | 指向 | 说明 |
  |---|---|---|
  | `relay.example.com` | 服务器 IP | 房主连的控制面（走 TLS） |
  | `*.mc.example.com` | 服务器 IP | 玩家入口，**必须 DNS only，不能挂 CDN** |

- 端口放行：**80、443、25565**（云厂商安全组 + 系统防火墙都要）

> 挂 Cloudflare 的话，`*.mc` 那条一定要是灰色云朵。橙云不代理 25565，玩家会连到 Cloudflare 的 IP 上然后失败。

### 一键安装

```bash
scp -r nonerp-bundle root@<服务器IP>:/root/nonerp
ssh root@<服务器IP>
bash /root/nonerp/setup.sh
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

### 手工安装（想自己控制每一步）

```bash
install -d /opt/nonerp
install -m 0755 nonerp-linux-amd64 /opt/nonerp/nonerp

cat > /opt/nonerp/server.yaml <<'YAML'
listen: "127.0.0.1:8080"          # 只给反代用，不直接对外
public_port: 25565                # 玩家连这个
domain_suffix: "mc.example.com"   # 玩家地址 = <房间码>.mc.example.com
public_url: "wss://relay.example.com"
grace_period: 60s
tokens: {}
YAML
chmod 600 /opt/nonerp/server.yaml

# 服务：内容见 deploy/nonerp.service
systemctl daemon-reload && systemctl enable --now nonerp

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

配置写在 **exe 同目录**的 `client.yaml`，日志写在同目录的 `nonerp.log`（10 MB 轮转，保留 3 份）。

命令行等价写法：

```powershell
.\NoneFrp-windows-amd64.exe client -join "nonerp://join?relay=...&token=..."
```

`-join` 连配置文件都不需要预先存在。

### 之后每次

双击 exe 就够了。

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
  code: auto             # auto = 由中继分配；也可以钉死一个
token: JOIN-XXXXXXXX
ui:
  enabled: true
  open_browser: true
```

命令行参数（都可选）：`-config`、`-relay`、`-token`、`-join`、`-debug`、`-check`。

### 界面

- **房间地址**：发给玩家的东西，带复制按钮
- **在线玩家 / 上行 / 下行**：实时刷新
- **本地服务器**：每 5 秒探测一次本地端口。显示「未监听」说明 Minecraft 服务端没开或端口不一致，页面会直接给出红色警告
- **直连地址**：局域网 IPv4、公网 IPv6，能直连的人可以绕过中继
- **设置**：改中继地址、邀请码、端口、房间码，保存后自动重连

界面只监听 `127.0.0.1`，路径里带随机串，并校验 `Host` 头，所以外网访问不到，别的网页也改不动它。

---

## 邀请码

在服务器上签发：

```bash
cd /opt/nonerp

./nonerp server invite --once --note "小明"      # 一台设备
./nonerp server invite --devices 5 --days 30     # 五台设备，30 天
./nonerp server invite --note "老张"             # 永久、不限设备
systemctl restart nonerp                         # 新码要重启才生效
```

- 输出形如 `JOIN-3X8P2R9T`，把它和 `relay.example.com` 一起发给房主
- 「一次性」指**一台设备**：同一台机器断线重连、重启电脑都不受影响
- 用掉多少台设备记在 `/opt/nonerp/state.json`，重启不丢

查看：

```bash
./nonerp server token ls -config server.yaml
# JOIN-3X8P2R9T  1/1 devices, never expires        小明
# NFRP-LEGACY22  unlimited devices, never expires   老张
```

撤销：删掉 `server.yaml` 里对应的那一行，重启服务。

> 邀请码等于权限。它不能顶掉别人的房间（房间码已绑定设备），但拿到的人可以在你这台中继上开房间、消耗带宽。

想让某个房主的地址固定下来：

```bash
./nonerp server link -config server.yaml -token JOIN-3X8P2R9T --room mcxiao
```

然后让他在界面「房间码」里填 `mcxiao`，地址就永远是 `mcxiao.mc.example.com`。

---

## 玩家地址与直连

玩家填：`<房间码>.mc.example.com:25565`，端口不能省。

同一局域网、或双方都有公网 IPv6 时，可以用界面上的**直连地址**，不经过中继：

| 地址 | 谁能用 |
|---|---|
| `192.168.x.x:25565` | 同一个路由器下的人 |
| `[2409:...]:25565` | 有公网 IPv6 的人（多数移动/联通宽带和手机流量） |

直连的额外好处：本地服务端天然能看到真实玩家 IP，也不需要开 `proxy_protocol`。

---

## 日常运维

```bash
systemctl status nonerp           # 状态
systemctl restart nonerp          # 改完配置
journalctl -u nonerp -f           # 实时日志
curl -s https://relay.example.com/readyz    # 健康检查，期望 ready
```

健康检查命令行工具：

```bash
./nonerp server -config server.yaml -check    # 只校验配置，不启动
```

升级：

```bash
# 传新二进制，验证后替换
install -m 0755 nonerp.new /opt/nonerp/nonerp
systemctl restart nonerp
```

房主端不需要跟着升级：协议向后兼容，中继可随时重启，房主端会自动重连。

每月建议看一眼：`df -h /`、`certbot certificates`、云厂商的流量用量（MC 是持续流量）。

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
| `https://relay.../readyz` 返回 502 | 中继没起来，`systemctl status nonerp`；多半是 `server.yaml` 写坏了 |
| 玩家能连但一直卡登录 | 服务器列表能刷新说明隧道通，问题在 Paper 侧 |
| 「一次性邀请码用过了」 | 换了机器或删了 `device_id`；把该码的 `max_devices` 改成 2 救急 |

服务器日志在 `journalctl`，房主日志在 exe 旁边的 `nonerp.log`。加 `-debug` 能看到每条流的字节数和每个拒绝原因。

---

## 从源码构建

需要 Go 1.26+。

```powershell
.\build.ps1 -Version 0.2.0
```

产物：

| 文件 | 用途 |
|---|---|
| `dist/NoneFrp-windows-amd64.exe` | 房主端 |
| `dist/nonerp-linux-amd64` | 中继（amd64） |
| `dist/nonerp-linux-arm64` | 中继（树莓派等） |
| `dist/nonerp-darwin-arm64` | 房主端（Mac） |
| `dist/nonerp-bundle/` | 上传到服务器的完整包（二进制 + 脚本 + 文档） |

测试：

```bash
gofmt -l . && go vet ./... && go test ./...
```

## 项目结构

```
cmd/nonerp/          单入口：server / client / server token|link|invite
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
