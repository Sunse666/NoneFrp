package ui

// indexHTML is the single page of the local interface. It is rendered with the
// template's Base value, the path segment that guards the interface.
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
    .address { background: #16181c !important; }
  }
  .wrap { max-width: 780px; margin: 0 auto; }
  header { display: flex; align-items: center; gap: 12px; margin-bottom: 22px; }
  h1 { font-size: 19px; margin: 0; font-weight: 650; letter-spacing: .2px; }
  .pill {
    font-size: 12px; padding: 3px 10px; border-radius: 999px;
    background: #e6e8eb; color: #4b5058; font-weight: 600;
  }
  .pill.on { background: #d7f2df; color: #17703a; }
  .pill.off { background: #fbe0df; color: #a3271f; }
  .card {
    background: #fff; border: 1px solid #e3e6ea; border-radius: 12px;
    padding: 18px 20px; margin-bottom: 14px;
  }
  .label { font-size: 12px; text-transform: uppercase; letter-spacing: .6px; color: #6b7280; margin-bottom: 6px; }
  .muted { color: #6b7280; font-size: 13px; }
  .address {
    display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
    font: 600 20px/1.4 ui-monospace, "Cascadia Mono", Consolas, monospace;
    background: #f7f8fa; border-radius: 8px; padding: 12px 14px; word-break: break-all;
  }
  .address span { flex: 1 1 auto; }
  button {
    font: inherit; font-size: 13px; padding: 7px 14px; cursor: pointer;
    border-radius: 8px; border: 1px solid #d2d7de; background: #fff; color: #1c1e21;
  }
  button:hover { border-color: #a9b1bc; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 14px; }
  .value { font: 600 22px/1.3 ui-monospace, "Cascadia Mono", Consolas, monospace; }
  .row { display: flex; justify-content: space-between; padding: 7px 0; border-bottom: 1px solid #eef0f3; font-size: 14px; }
  .row:last-child { border-bottom: 0; }
  .row .k { color: #6b7280; }
  .error { display: none; margin-top: 10px; color: #a3271f; font-size: 13px; }
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
    <div class="muted" id="hint">把这个地址发给朋友，在 Minecraft 里直接添加服务器。</div>
    <div class="error" id="error"></div>
  </section>

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
    <div class="row"><span class="k">真实 IP 透传</span><span id="proxy">—</span></div>
    <div class="row"><span class="k">设备标识</span><span id="device">—</span></div>
  </section>

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

    var address = s.public_host && s.public_port
      ? s.public_host + ':' + s.public_port
      : '等待注册…';
    setText('address', address);

    setText('players', s.players || 0);
    setText('up', formatBytes(s.bytes_up));
    setText('down', formatBytes(s.bytes_down));
    setText('relay', s.relay_url || '—');
    setText('room', s.room_code || '—');
    setText('local', s.local_port ? '127.0.0.1:' + s.local_port : '—');
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
    var done = function () {
      var button = document.getElementById('copy');
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
