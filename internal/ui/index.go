package ui

// indexHTML is the single page of the local interface. It is rendered with the
// pageData assembled by buildPage.
const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>NoneFrp</title>
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 32px 20px;
    font: 15px/1.6 -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif;
    background: #f5f6f8; color: #1c1e21;
  }
  @media (prefers-color-scheme: dark) {
    body { background: #16181c; color: #e6e8eb; }
    .card { background: #20232a !important; border-color: #2c313a !important; }
    .muted { color: #8b929c !important; }
    .address, input[type=text], input[type=number], input[type=password], textarea {
      background: #16181c !important; color: #e6e8eb !important; border-color: #333a44 !important;
    }
    button { background: #2a2f38; color: #e6e8eb; border-color: #3a414d; }
  }
  .wrap { max-width: 780px; margin: 0 auto; }
  header { display: flex; align-items: center; gap: 12px; margin-bottom: 22px; }
  h1 { font-size: 19px; margin: 0; font-weight: 650; letter-spacing: .2px; }
  .pill { font-size: 12px; padding: 3px 10px; border-radius: 999px; background: #e6e8eb; color: #4b5058; font-weight: 600; }
  .pill.on { background: #d7f2df; color: #17703a; }
  .pill.off { background: #fbe0df; color: #a3271f; }
  .card { background: #fff; border: 1px solid #e3e6ea; border-radius: 12px; padding: 18px 20px; margin-bottom: 14px; }
  .label { font-size: 12px; text-transform: uppercase; letter-spacing: .6px; color: #6b7280; margin-bottom: 10px; }
  .muted { color: #6b7280; font-size: 13px; }
  .address {
    display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
    font: 600 20px/1.4 ui-monospace, "Cascadia Mono", Consolas, monospace;
    background: #f7f8fa; border-radius: 8px; padding: 12px 14px; word-break: break-all;
    margin-bottom: 8px;
  }
  .address span { flex: 1 1 auto; }
  button {
    font: inherit; font-size: 13px; padding: 7px 14px; cursor: pointer;
    border-radius: 8px; border: 1px solid #d2d7de; background: #fff; color: #1c1e21;
  }
  button:hover { border-color: #a9b1bc; }
  button.primary { background: #2f6feb; border-color: #2f6feb; color: #fff; font-weight: 600; }
  button.primary:hover { background: #2559c4; border-color: #2559c4; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 14px; }
  .value { font: 600 22px/1.3 ui-monospace, "Cascadia Mono", Consolas, monospace; }
  .row { display: flex; justify-content: space-between; gap: 12px; padding: 7px 0; border-bottom: 1px solid #eef0f3; font-size: 14px; }
  .row:last-child { border-bottom: 0; }
  .row .k { color: #6b7280; }
  .error { color: #a3271f; font-size: 13px; margin-top: 8px; }
  .flash { border-radius: 8px; padding: 10px 12px; font-size: 14px; margin-bottom: 14px; }
  .flash.ok { background: #e8f6ed; color: #17703a; }
  .flash.err { background: #fdecea; color: #a3271f; }
  .field { margin-bottom: 12px; }
  .field > span { display: block; font-size: 13px; color: #6b7280; margin-bottom: 5px; }
  input[type=text], input[type=number], input[type=password], textarea {
    width: 100%; font: inherit; font-size: 14px; padding: 9px 11px;
    border: 1px solid #d2d7de; border-radius: 8px; background: #fff; color: inherit;
  }
  textarea { resize: vertical; min-height: 68px; font-family: ui-monospace, "Cascadia Mono", Consolas, monospace; }
  label.check { display: flex; align-items: center; gap: 8px; font-size: 14px; }
  .actions { display: flex; align-items: center; gap: 12px; margin-top: 4px; flex-wrap: wrap; }
  footer { margin-top: 24px; text-align: center; font-size: 12px; color: #8b929c; }
</style>
</head>
<body>
<div class="wrap">
  <header>
    <h1>NoneFrp</h1>
    <span class="pill" id="state">连接中…</span>
  </header>

  <section class="card">
    <div class="label">房间地址</div>
    <div class="address">
      <span id="address">等待注册…</span>
      <button id="copy" type="button">复制</button>
    </div>
    <div class="muted">把这个地址发给朋友，在 Minecraft 里直接添加服务器。</div>
    <div class="error" id="error" style="display:none"></div>
    <div class="error" id="localwarn" style="display:none"></div>
  </section>

  {{if .Direct}}
  <section class="card">
    <div class="label">直连地址（不经过中继）</div>
    {{range .Direct}}<div class="row"><span class="k">·</span><span>{{.}}</span></div>{{end}}
    <div class="muted" style="margin-top:8px">
      同一局域网的朋友用第一个地址最快。公网地址要求对方的网络也能直连过来，通常需要路由器放行、或对方具备 IPv6；连不通就走上面的中继地址。
    </div>
  </section>
  {{end}}

  <div class="grid">
    <section class="card">
      <div class="label">在线玩家</div>
      <div class="value" id="players">0</div>
    </section>
    <section class="card">
      <div class="label">上行</div>
      <div class="value" id="up">0 B</div>
    </section>
    <section class="card">
      <div class="label">下行</div>
      <div class="value" id="down">0 B</div>
    </section>
  </div>

  <section class="card">
    <div class="label">运行状态</div>
    <div class="row"><span class="k">中继服务器</span><span id="relay">—</span></div>
    <div class="row"><span class="k">房间码</span><span id="room">—</span></div>
    <div class="row"><span class="k">本地端口</span><span id="local">—</span></div>
    <div class="row"><span class="k">本地服务器</span><span id="localok">探测中…</span></div>
    <div class="row"><span class="k">真实 IP 透传</span><span id="proxy">—</span></div>
    <div class="row"><span class="k">设备标识</span><span id="device">—</span></div>
  </section>

  {{if .Editable}}
  <section class="card">
    <div class="label">设置</div>
    {{if not .Configured}}
    <div class="flash err">还没有连上中继。把管理员给你的 <b>nonerp://join?...</b> 链接粘贴到第一个框，直接用下面的「保存并重连」即可；也可以手动填写中继地址和令牌。</div>
    {{end}}
    {{if .Message}}<div class="flash {{if .IsError}}err{{else}}ok{{end}}">{{.Message}}</div>{{end}}
    <form method="post" action="{{.Base}}/settings">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="field">
        <span>邀请链接（可选，粘贴后自动填好下面各项）</span>
        <input type="text" name="join" placeholder="nonerp://join?..." autocomplete="off" spellcheck="false">
      </div>
      <div class="field">
        <span>中继地址（每行一个，按顺序尝试）</span>
        <textarea name="relays" spellcheck="false">{{.Relays}}</textarea>
      </div>
      <div class="field">
        <span>访问令牌（留空保持不变）</span>
        <input type="password" name="token" placeholder="{{.Token}}" autocomplete="off" spellcheck="false">
      </div>
      <div class="field">
        <span>本地服务器端口</span>
        <input type="number" name="local_port" min="1" max="65535" value="{{.LocalPort}}">
      </div>
      <div class="field">
        <span>房间码（auto 表示由中继分配）</span>
        <input type="text" name="room_code" value="{{.RoomCode}}" spellcheck="false">
      </div>
      <div class="field">
        <label class="check">
          <input type="checkbox" name="proxy_protocol" {{if .ProxyProtocol}}checked{{end}}>
          向本地服务器发送真实玩家 IP（需要服务端开启 proxy-protocol）
        </label>
      </div>
      <div class="actions">
        <button type="submit" class="primary">保存并重连</button>
        <span class="muted">保存后立刻用新配置重连，正在游戏的玩家会短暂断开。</span>
      </div>
    </form>
  </section>
  {{end}}

  <footer>界面只监听本机回环地址，关闭窗口不会中断联机。</footer>
</div>

<script>
  var base = {{.Base}};
  var timer = null;

  function formatBytes(n) {
    if (!n) return '0 B';
    var units = ['B', 'KB', 'MB', 'GB'];
    var i = 0;
    while (n >= 1024 && i < units.length - 1) { n = n / 1024; i++; }
    return (i === 0 ? n : n.toFixed(1)) + ' ' + units[i];
  }

  function setText(id, value) {
    var el = document.getElementById(id);
    if (el) el.textContent = value;
  }

  function render(s) {
    var state = document.getElementById('state');
    state.textContent = s.connected ? '已连接' : '未连接';
    state.className = 'pill ' + (s.connected ? 'on' : 'off');

    var address = s.public_host && s.public_port ? s.public_host + ':' + s.public_port : '等待注册…';
    setText('address', address);
    setText('players', s.players || 0);
    setText('up', formatBytes(s.bytes_up));
    setText('down', formatBytes(s.bytes_down));
    setText('relay', s.relay_url || '—');
    setText('room', s.room_code || '—');
    setText('local', s.local_port ? '127.0.0.1:' + s.local_port : '—');
    setText('localok', s.local_ok ? '正常监听' : '未监听');

    var localwarn = document.getElementById('localwarn');
    if (s.local_ok === false) {
      localwarn.textContent = '本地 Minecraft 服务器没有在 127.0.0.1:' + s.local_port + ' 上监听，玩家连进来会失败。请确认服务端已启动，且端口和这里一致。';
      localwarn.style.display = 'block';
    } else {
      localwarn.style.display = 'none';
    }
    setText('proxy', s.proxy_protocol ? '已开启' : '未开启');
    setText('device', (s.device_id || '').slice(0, 8) || '—');

    var error = document.getElementById('error');
    if (s.last_error) {
      error.textContent = '最近一次错误：' + s.last_error;
      error.style.display = 'block';
    } else {
      error.style.display = 'none';
    }
  }

  function tick() {
    fetch(base + '/api/status', { cache: 'no-store' })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (s) { if (s) render(s); })
      .catch(function () {})
      .then(function () { timer = setTimeout(tick, 2000); });
  }

  document.getElementById('copy').addEventListener('click', function () {
    var text = document.getElementById('address').textContent;
    var button = document.getElementById('copy');
    var done = function () {
      button.textContent = '已复制';
      setTimeout(function () { button.textContent = '复制'; }, 1200);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, done);
    } else {
      done();
    }
  });

  tick();
</script>
</body>
</html>
`
