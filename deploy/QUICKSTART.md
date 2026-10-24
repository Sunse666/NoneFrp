# 一页纸：把中继装到服务器上

## 上传什么

只上传**这一个文件夹**（`dist/nonefrp-bundle`），里面已经包含二进制和全部脚本。

```powershell
# Windows 上，仓库根目录
scp -r .\dist\nonefrp-bundle root@你的服务器IP:/root/nonefrp
```

## 服务器上做什么

```bash
ssh root@你的服务器IP
bash /root/nonefrp/setup.sh
```

会看到一个菜单。第一次直接按 **0**（一键执行 1-8），等它跑完。

一切正常的话，菜单上的 1-8 全部变成 `✓`。

## 然后签发邀请码

在菜单里按 **9**，它会：

1. 签发一个邀请码（只允许一台设备）
2. 让中继读到它（SIGHUP，不断开已连着的房主）
3. 打印成两行，外加一条链接

两种发法都行，看房主怎么方便：

```
  中继地址: relay.你的域名
  邀请码:   JOIN-3X8P2R9T

或者整条链接
nonefrp://join?relay=wss%3A%2F%2Frelay.你的域名&room=auto&token=JOIN-3X8P2R9T
```

## 房主那边

**双击 exe** 就行。浏览器会打开界面，把上面那两行填进去（或者直接粘贴整条链接），点「测试连接」确认，再点「保存并重连」。

命令行等价写法：

```powershell
.\NoneFrp-windows-amd64.exe client -join "粘贴那行链接"
```

界面上显示的房间地址就是发给玩家的东西，形如：

```
7fcrfu.mc.你的域名:25565
```

**建议**：在设置里把「房间码」填死（比如 `mcfriends`），地址就永远不变了——否则中继重装或换配置后可能换码，玩家存的地址就失效了。

## 两件脚本做不了的事

| 事项 | 在哪做 |
| --- | --- |
| 安全组放行 TCP 25565 | 云厂商控制台 |
| DNS：`relay.域名` 和 `*.mc.域名` → 服务器 IP | 域名服务商；`*.mc` 必须是灰色云朵，不能开 CDN |

## 装完之后

日常维护看同目录的 `OPERATIONS.md`。

常用就三条：

```bash
systemctl status nonefrp        # 活着吗
journalctl -u nonefrp -f        # 谁连进来了
bash /root/nonefrp/setup.sh     # 回菜单，看哪一步红了
```

## 出问题了

重新运行 `bash /root/nonefrp/setup.sh`，菜单上没打勾的那一步就是问题所在，下面会写出原因和处理办法。

要全部拆掉：菜单里按 **r**。
