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
mkdir -p /opt/nonerp
install -m 0755 /tmp/nonerp /opt/nonerp/nonerp

# 2. 配置
cat > /opt/nonerp/server.yaml <<'EOF'
listen: "127.0.0.1:8080"
public_port: 25565
domain_suffix: "mc.example.com"
public_url: "wss://relay.example.com"
grace_period: 60s
tokens: {}
EOF

# 3. systemd 单元（内容见 deploy/nonerp.service）
systemctl daemon-reload && systemctl enable --now nonerp

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
systemctl status nonerp          # 看状态
systemctl restart nonerp         # 改完配置后重启
journalctl -u nonerp -f          # 实时日志（看谁连进来、谁掉线）
curl -s https://relay.example.com/readyz   # 健康检查，期望 ready
```

改配置的固定流程：**改 `/opt/nonerp/server.yaml` → `systemctl restart nonerp` → `curl readyz`**。

## 签发新 token（给一个新房主）

```bash
cd /opt/nonerp
./nonerp server token new -config server.yaml -note "谁在用"
systemctl restart nonerp            # token 是启动时读的，必须重启
./nonerp server link -config server.yaml -token NFRP-XXXXXXXX
```

第二条命令打印的 `nonerp://join?...` 链接就是要发给房主的东西。

**撤销一个 token**：把 `server.yaml` 里 `tokens:` 下面那一行删掉，然后 `systemctl restart nonerp`。

## 升级

```powershell
# Windows 上，仓库根目录
scp .\dist\nonerp-linux-amd64 root@<server-ip>:/opt/nonerp/nonerp.new
```

```bash
# 服务器上
cd /opt/nonerp
./nonerp.new version          # 确认版本号
file nonerp.new               # 必须是 ELF x86-64
install -m 0755 nonerp.new nonerp
rm -f nonerp.new
systemctl restart nonerp
curl -s https://relay.example.com/readyz
```

房主端不用动，会自动重连。

## 关键文件和端口

| 项 | 位置 |
| --- | --- |
| 二进制 | `/opt/nonerp/nonerp` |
| 配置（含 token） | `/opt/nonerp/server.yaml` |
| systemd 单元 | `/etc/systemd/system/nonerp.service` |
| nginx 站点 | `/etc/nginx/conf.d/relay.conf` 或 `/etc/nginx/sites-available/relay.conf` |
| 监听 | `127.0.0.1:8080`（内部）、`:25565`（玩家） |
| 外部端口 | 80、443、25565 |

## 排障

| 现象 | 原因 |
| --- | --- |
| `curl` 返回 **502** | 中继没跑：`systemctl status nonerp`，多半是 `server.yaml` 写坏了 |
| 服务反复重启，日志 `yaml: line N` | 配置缩进错。YAML 禁用 Tab，块状结构用两个空格 |
| `bind: address already in use` | 已经有一个实例（通常是 systemd 那个）占着 25565，别再手动跑 |
| 玩家提示**房间不存在** | 地址里的房间码和房主端当前房间码不一致 |
| 房主端一直 `relay attempt failed` | 证书没过、nginx 缺 `Upgrade` 头、或 token 不在 `tokens:` 里 |
| 证书到期 | `certbot renew --dry-run` 验证自动续期 |

---

# 二、Windows 房主端

## 首次配置

最简单的路径：**双击 exe**。浏览器会自动打开，把管理员给的 `nonerp://join?...` 链接粘贴到「邀请链接」框，点「保存并重连」。配置写进 exe 同目录的 `client.yaml`，下次双击直接连。

命令行方式（等价）：

1. 拿到 exe：`dist\NoneFrp-windows-amd64.exe`
2. 拿到中继管理员发的 `nonerp://join?...` 链接
3. 在 PowerShell 里执行（**路径开头的 `.` 不能省**）：

```powershell
cd D:\IO\Go_obj\NoneFrp
.\dist\NoneFrp-windows-amd64.exe client -join "nonerp://join?relay=..."
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
| 玩家进不来，但界面显示已连接 | 检查 Paper 端口是否和「本地端口」一致 |
| 房间码一直在变 | `client.yaml` 或数据目录换了位置，device id 跟着变了 |

日志在配置文件同目录；需要看细节时加 `-debug`：

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
