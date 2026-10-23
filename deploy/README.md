# Deploying the relay

The relay is one static binary plus one YAML file. It needs a public IPv4
address; nothing else is required on the machine.

## Install

```powershell
scp dist/nonerp-linux-amd64 deploy/setup.sh root@relay:/root/
ssh root@relay 'bash /root/setup.sh'
```

`setup.sh` is a menu. Each of the nine steps shows a tick when it is already
done, can be re-run on its own, and prints the reason and the fix when it
fails. **0** runs steps 1-8 in one go, **9** issues the first token and prints
the join link, **r** rolls everything back, **q** exits.

```
 [✓] 1  环境检查
 [ ] 2  安装二进制
 ...
   0  一键执行 1-8
   t  修改域名 / 控制面 / 邮箱
   l  查看服务日志
   r  回滚（卸载）
   q  退出
```

Non-interactive (CI, or you already know the answers):

```bash
bash setup.sh --domain mc.example.com --auto
```

Every step is safe to repeat: an existing `server.yaml` keeps its tokens, and
a broken nginx configuration is rolled back so the machine's website keeps
serving.

Options: `--relay=`, `--email=`, `--port=`, `--bin=`, `--force`,
`--no-tls`.

Two things it cannot do for you: opening the port in the cloud vendor's
security group, and the DNS records. See below.

## What the box has to allow

| Port | TCP | Why |
| --- | --- | --- |
| 80 | yes | certificate issuance and renewal (Caddy) |
| 443 | yes | agent control plane over TLS |
| 25565 | yes | players connect here |

Open all three in the cloud vendor's security group **and** in the host
firewall. The security group is the one people forget.

## If the box already runs a website

Nothing has to be moved. The relay adds two things: a loopback listener on
8080 that your existing web server proxies to, and its own listener on 25565.

Start with the preflight, which only reads:

```bash
scp deploy/preflight.sh root@relay:/tmp/
ssh root@relay 'bash /tmp/preflight.sh'
```

It reports the listeners on 80/443/25565, which web server is installed, the
firewall state and the public addresses. Two things to watch for:

- **nginx**: use `deploy/nginx-relay.conf`. The websocket upgrade headers are
  not optional, the agent's control connection dies without them.
- **Cloudflare in front of the site**: the `*.mc.example.com` record must be
  **DNS only (grey cloud)**. Players connect to port 25565, which Cloudflare
  does not proxy, so a proxied record hands out an address that cannot work.

## Steps

1. **Install the binary**

   ```bash
   scp dist/nonerp-linux-amd64 root@relay:/tmp/nonerp
   ssh root@relay 'install -d /opt/nonerp && install -m 0755 /tmp/nonerp /opt/nonerp/nonerp'
   ```

2. **Write the configuration**

   ```bash
   scp configs/server.example.yaml root@relay:/opt/nonerp/server.yaml
   ssh root@relay 'nano /opt/nonerp/server.yaml'
   ```

   Fill in `domain_suffix` (the name players will type) and `public_url` (the
   websocket address agents use). Leave `listen` on loopback: Caddy terminates
   TLS in front of it.

3. **Install the service**

   ```bash
   scp deploy/nonerp.service root@relay:/etc/systemd/system/
   ssh root@relay 'systemctl daemon-reload && systemctl enable --now nonerp'
   ssh root@relay 'journalctl -u nonerp -n 20 --no-pager'
   ```

4. **Put Caddy in front of it**

   ```bash
   ssh root@relay 'apt install -y caddy'
   scp deploy/Caddyfile root@relay:/etc/caddy/Caddyfile
   ssh root@relay 'systemctl reload caddy'
   ```

   Caddy issues and renews the certificate on its own, which needs ports 80
   and 443 reachable from the internet.

5. **Point DNS at the box**

   ```
   relay.example.com      A     <vps ip>
   *.mc.example.com       A     <vps ip>
   ```

   The wildcard record is what lets `a2b3c4.mc.example.com` reach the relay
   without creating a record per room.

   No domain yet? `<room>.<vps ip>.nip.io` resolves too, but certificates for
   `nip.io` names are shared and rate limited, so treat it as a stopgap.

6. **Issue a token and hand it out**

   ```bash
   ssh root@relay '/opt/nonerp/nonerp server token new -config /opt/nonerp/server.yaml -note "whoever"'
   ssh root@relay '/opt/nonerp/nonerp server link -config /opt/nonerp/server.yaml -token NFRP-XXXXXXXX'
   ```

   The second command prints a `nonerp://join?...` link to send to the host.

7. **Check it**

   ```bash
   curl -s http://127.0.0.1:8080/readyz     # ready
   ss -lntp | grep -E ':(443|25565)'        # both listening
   ```

## Upgrading

```bash
scp dist/nonerp-linux-amd64 root@relay:/opt/nonerp/nonerp.new
ssh root@relay 'install -m 0755 /opt/nonerp/nonerp.new /opt/nonerp/nonerp && systemctl restart nonerp'
```

Agents reconnect on their own within seconds; players see a brief "room
temporarily offline" instead of a broken address.

## When something does not work

| Symptom | Check |
| --- | --- |
| Agent cannot connect | `journalctl -u caddy`, certificate issued? DNS pointing at this box? |
| Players cannot connect | Security group and host firewall for 25565; `ss -lntp` shows 25565? |
| `bind: permission denied` | Port 25565 is above 1024, so this is usually another process holding it. |
| "room not found" in the server list | The host typed the wrong room code, or `domain_suffix` does not match. |
