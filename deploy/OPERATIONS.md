# 运维手册

本文按「谁在操作」分成两部分。日常 95% 的操作都在 Windows 那台机器上，Linux 服务器装好之后基本不用碰。

## 这套东西由三部分组成

| 部分 | 跑在哪 | 谁维护 | 作用 |
| --- | --- | --- | --- |
| 中继 | Linux VPS（你的服务器 IP） | 你 | 接住玩家的连接，转给房主 |
| 房主端 | Windows（开 Paper 的那台） | 房主 | 连中继、把流量转给本机 Paper |
| 玩家 | 无 | 没人 | 在 MC 里填一个地址 |

---

# 一、Linux 服务器（中继）

## 首次部署（一次性）

已经完成，留档备查。全部命令在服务器上执行。

```bash
# 1. 二进制
mkdir -p /opt/nonefrp
install -m 0755 /tmp/nonefrp /opt/nonefrp/nonefrp

# 2. 配置
cat > /opt/nonefrp/server.yaml <<'EOF'
listen: "127.0.0.1:8080"
public_port: 25565
domain_suffix: "mc.example.com"
public_url: "wss://relay.example.com"
grace_period: 60s
tokens: {}
EOF

# 3. systemd 单元（内容见 deploy/nonefrp.service）
systemctl daemon-reload && systemctl enable --now nonefrp

# 4. nginx 反代（内容见 deploy/nginx-relay.conf，注意 Upgrade 两行）
#    宝塔面板则在面板里加站点 + 反向代理

# 5. 证书
certbot --nginx -d relay.example.com

# 6. 放行端口（安全组里也要加）
ufw allow 25565/tcp
```

DNS 需要两条 A 记录，都指向 <server-ip>：

- `relay.example.com` —— 房主端连的控制面
- `*.mc.example.com` —— 玩家入口，**必须灰色云朵（DNS only）**

## 日常操作

```bash
systemctl status nonefrp          # 看状态
systemctl restart nonefrp         # 改完配置后重启
journalctl -u nonefrp -f          # 实时日志（看谁连进来、谁掉线）
curl -s https://relay.example.com/readyz   # 健康检查，期望 ready

# 只改了 tokens: 时用它，不断线
kill -HUP $(systemctl show -p MainPID --value nonefrp)

# 指标（需要凭证）
curl -s -H "Authorization: Bearer NFRP-XXXX" https://relay.example.com/metrics
```

改配置分两种：

- **加/删邀请码** → `kill -HUP`，在线房主不掉线
- **改端口、域名、监听地址** → `systemctl restart nonefrp`，房主会断约 1 秒后自动重连

## 签发邀请码（给一个新朋友）

```bash
cd /opt/nonefrp
./nonefrp server invite --once --note "小明"        # 只允许一台设备
#  invite:  JOIN-3X8P2R9T
#  policy:  0/1 devices, never expires
kill -HUP $(systemctl show -p MainPID --value nonefrp)   # 不用重启
```

房主在界面里填两个东西：中继地址（`relay.example.com`）+ 邀请码（`JOIN-3X8P2R9T`），点「测试连接」确认，再点「保存并重连」。

- `--devices 5` 允许 5 台设备，`--days 30` 30 天后失效
- 不加任何参数 = 永久、不限设备（等价于老的 token）
- 「一次性」指的是**一台设备**：同一台机器断线重连不受影响

查看谁用掉了哪个码：

```bash
./nonefrp server token ls -config server.yaml
# JOIN-3X8P2R9T  1/1 devices, never expires        小明
# NFRP-LEGACY22  unlimited devices, never expires   老张
```

用掉记录在 `/opt/nonefrp/state.json`，重启不丢。

## 签发新 token（给一个新房主）

```bash
cd /opt/nonefrp
./nonefrp server token new -config server.yaml -note "谁在用"
systemctl restart nonefrp            # token 是启动时读的，必须重启
./nonefrp server link -config server.yaml -token NFRP-XXXXXXXX
```

第二条命令打印的 `nonefrp://join?...` 链接就是要发给房主的东西。

**一次给多个房主**（一人一个 token，各发一条链接）：

```bash
cd /opt/nonefrp
./nonefrp server token new -config server.yaml -note "小明"
./nonefrp server token new -config server.yaml -note "小红"
systemctl restart nonefrp                 # 一次性重启，不用每签一个就重启
./nonefrp server link -config server.yaml -all
```

`-all` 会按 note 排序，把每个 token 对应的链接都打出来。

**看清谁在用**：`./nonefrp server token ls -config server.yaml`

**撤销一个 token**：把 `server.yaml` 里 `tokens:` 下面那一行删掉，然后 `systemctl restart nonefrp`。

> 链接本身就是凭证：谁拿到谁就能在这台中继上开一个房间、占用带宽。但它**不能顶掉别人的房间**——房间码已经被别的设备占用时注册会被拒绝。

## 升级

```powershell
# Windows 上，仓库根目录
scp .\dist\nonefrp-linux-amd64 root@<server-ip>:/opt/nonefrp/nonefrp.new
```

```bash
# 服务器上
cd /opt/nonefrp
./nonefrp.new version          # 确认版本号
file nonefrp.new               # 必须是 ELF x86-64
install -m 0755 nonefrp.new nonefrp
rm -f nonefrp.new
systemctl restart nonefrp
curl -s https://relay.example.com/readyz
```

房主端不用动，会自动重连。

## 关键文件和端口

| 项 | 位置 |
| --- | --- |
| 二进制 | `/opt/nonefrp/nonefrp` |
| 配置（含 token） | `/opt/nonefrp/server.yaml` |
| systemd 单元 | `/etc/systemd/system/nonefrp.service` |
| nginx 站点 | `/etc/nginx/conf.d/relay.conf` 或 `/etc/nginx/sites-available/relay.conf` |
| 监听 | `127.0.0.1:8080`（内部）、`:25565`（玩家） |
| 外部端口 | 80、443、25565 |

## 排障

| 现象 | 原因 |
| --- | --- |
| `curl` 返回 **502** | 中继没跑：`systemctl status nonefrp`，多半是 `server.yaml` 写坏了 |
| 服务反复重启，日志 `yaml: line N` | 配置缩进错。YAML 禁用 Tab，块状结构用两个空格 |
| `bind: address already in use` | 已经有一个实例（通常是 systemd 那个）占着 25565，别再手动跑 |
| 玩家提示**房间不存在** | 地址里的房间码和房主端当前房间码不一致 |
| 房主端一直 `relay attempt failed` | 证书没过、nginx 缺 `Upgrade` 头、或 token 不在 `tokens:` 里 |
| 证书到期 | `certbot renew --dry-run` 验证自动续期 |

---

# 二、Windows 房主端

## 首次配置

最简单的路径：**双击 exe**。浏览器会自动打开，把管理员给的 `nonefrp://join?...` 链接粘贴到「邀请链接」框，点「保存并重连」。配置写进 exe 同目录的 `client.yaml`，下次双击直接连。

命令行方式（等价）：

1. 拿到 exe：`dist\NoneFrp-windows-amd64.exe`
2. 拿到中继管理员发的 `nonefrp://join?...` 链接
3. 在 PowerShell 里执行（**路径开头的 `.` 不能省**）：

```powershell
cd D:\IO\Go_obj\NoneFrp
.\dist\NoneFrp-windows-amd64.exe client -join "nonefrp://join?relay=..."
```

第一次运行会自动生成 `client.yaml`（和 exe 同目录）。之后就不用再带 `-join` 了：

```powershell
.\dist\NoneFrp-windows-amd64.exe client
```

浏览器会自动打开界面，状态显示「已连接」，房间地址形如：

```
a2b3c4.mc.example.com:25565
```

这个地址就是发给玩家的东西。

## 日常操作

| 想做的事 | 怎么做 |
| --- | --- |
| 看状态 / 房间地址 | 打开界面（启动时自动弹出） |
| 改配置 | 界面 → 设置 → 改完点「保存并重连」 |
| 给别人新地址 | 界面上的地址复制按钮 |
| 临时关掉联机 | 关掉这个窗口 |
| 看它有没有在跑 | 任务管理器里找 `NoneFrp-windows-amd64.exe` |

界面上的字段含义：

| 字段 | 含义 |
| --- | --- |
| 中继服务器 | 连的是哪台中继（`wss://relay.example.com`） |
| 房间码 | 玩家地址的第一段（`a2b3c4`） |
| 本地端口 | 你 Paper 的端口，默认 25565 |
| 真实 IP 透传 | 开启后 Paper 能看到玩家真实 IP（需要 Paper 端也配） |
| 直连地址 | 同一局域网 / IPv6 玩家可以不经过中继直接连 |

## 开机自启（可选）

程序暂时没有自带这个功能，用系统任务计划：

```powershell
schtasks /create /tn "NoneFrp" /tr ""D:\IO\Go_obj\NoneFrp\dist\NoneFrp-windows-amd64.exe" client" /sc onlogon
```

删除：`schtasks /delete /tn "NoneFrp"`（加 `/f` 不提示）。

## 排障

| 现象 | 原因 |
| --- | --- |
| `read client.yaml: no such file` | 没带 `-join`，也没手动创建配置 |
| 界面显示「未连接」，红字提示 401 | token 不对，或中继端删掉了这个 token |
| 界面显示「未连接」+ 连接被拒 | 中继地址写错 / 中继没跑 |
| 界面「本地服务器」显示**未监听** | Minecraft 服务端没启动，或端口和配置不一致 |
| 玩家进不来，但界面显示已连接 | 先看上一行的「本地服务器」状态 |
| 房间码一直在变 | `client.yaml` 或数据目录换了位置，device id 跟着变了 |

界面每 5 秒探测一次 `127.0.0.1:<本地端口>`，「本地服务器」那一行显示「未监听」时，页面会在地址下面直接给出警告——这条能挡掉大部分"玩家进不来"。

日志同时写到 **exe 同目录的 `nonefrp.log`**（10 MB 轮转，保留 3 份），关掉黑窗口也还在。需要看细节时加 `-debug`：

```powershell
.\dist\NoneFrp-windows-amd64.exe client -debug
```

---

# 三、玩家

在 MC 里填，注意**端口不能省**：

```
a2b3c4.mc.example.com:25565
```

同一个局域网的人也可以用界面上的「直连地址」，延迟更低、不消耗中继流量。

---

# 四、每月检查

- [ ] `curl -s https://relay.example.com/readyz` 返回 ready
- [ ] 服务器磁盘没满：`df -h /`
- [ ] 证书还在有效期内：`certbot certificates`
- [ ] 清一下不需要的 token
- [ ] 看一眼 VPS 的流量用量（MC 是持续流量，别月底才发现超额）
