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

## 然后签发 token

在菜单里按 **9**，它会：

1. 签发一个 token
2. 重启服务
3. 打印一行 `nonefrp://join?...` 链接

**把这一行复制下来，发给房主。**

## 房主那边

```powershell
.\NoneFrp-windows-amd64.exe client -join "粘贴刚才那行链接"
```

浏览器会自动打开，界面上显示的房间地址就是发给玩家的东西，形如：

```
a2b3c4.mc.你的域名:25565
```

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
