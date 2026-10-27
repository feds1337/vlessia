"use strict";

const api = () => window.go.main.App;
const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => document.querySelectorAll(sel);

let state = null;
let upd = null;
let platform = "windows"; // "windows" | "darwin" | "linux" | "android"
const pings = {};
let logsTimer = null;
let logsBusy = false;

// ---------- helpers ----------

function toast(msg, isError = false) {
  const el = document.createElement("div");
  el.className = "toast" + (isError ? " error" : "");
  el.textContent = msg;
  $("#toasts").appendChild(el);
  setTimeout(() => {
    el.classList.add("leaving");
    setTimeout(() => el.remove(), 200);
  }, isError ? 7000 : 3000);
}

async function call(fn, ...args) {
  try {
    return await api()[fn](...args);
  } catch (e) {
    const msg = String(e?.message ?? e);
    if (msg.includes("need_admin")) {
      $("#admin-dialog").showModal();
    } else if (msg.includes("need_ovpn_auth")) {
      askCredentials(state.status.serverId || state.selected, true);
    } else {
      toast(msg, true);
    }
    throw e;
  }
}

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function fmtSpeed(b) {
  if (b < 1024) return b + " Б/с";
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + " КБ/с";
  return (b / 1024 / 1024).toFixed(1) + " МБ/с";
}

function fmtBytes(b) {
  const u = ["Б", "КБ", "МБ", "ГБ", "ТБ"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return (i ? b.toFixed(1) : b) + " " + u[i];
}

function fmtDate(unix) {
  return new Date(unix * 1000).toLocaleString("ru-RU", { day: "numeric", month: "long", hour: "2-digit", minute: "2-digit" });
}

function plural(n, one, few, many) {
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

const bareVersion = (v) => String(v || "").replace(/^v/, "");

// The Telegram bot offered on first run and when there are no servers.
const BOT_URL = "https://t.me/v4hood_bot";

// ---------- servers ----------

const PROTO = {
  vless: "VLESS", vmess: "VMess", trojan: "Trojan", shadowsocks: "Shadowsocks", hysteria2: "Hysteria 2",
  amneziawg: "AmneziaWG", wireguard: "WireGuard", openvpn: "OpenVPN", socks: "SOCKS5", http: "HTTP",
};
const TRANSPORT = { ws: "WebSocket", grpc: "gRPC", xhttp: "XHTTP", httpupgrade: "HTTPUpgrade" };
const SECURITY = { reality: "REALITY", tls: "TLS" };

// protoLine describes a server in words, e.g. "VLESS / XHTTP / REALITY".
function protoLine(s) {
  const parts = [s.accessKeyUrl ? "Outline" : PROTO[s.protocol] || s.protocol];
  if (s.ovpn) parts.push(s.network.toUpperCase());
  if (!s.wg && !s.ovpn) {
    if (TRANSPORT[s.network]) parts.push(TRANSPORT[s.network]);
    if (SECURITY[s.security] && s.protocol !== "hysteria2") parts.push(SECURITY[s.security]);
    if (s.obfs) parts.push(s.obfs);
    if (s.hopPorts) parts.push("port hopping");
  }
  return parts.join(" / ");
}

function pingCell(id) {
  const ms = pings[id];
  if (ms === undefined) return `<span class="ping"></span>`;
  if (ms === null) return `<span class="ping">…</span>`;
  if (ms === -2) return `<span class="ping" title="Этот протокол не отвечает на проверку — проверьте подключением">—</span>`;
  if (ms < 0) return `<span class="ping bad">нет ответа</span>`;
  const cls = ms < 150 ? "good" : ms < 400 ? "mid" : "bad";
  return `<span class="ping ${cls}">${ms} мс</span>`;
}

function subTitle(sub) {
  return sub?.name || sub?.title || "Подписка";
}

function daysLeft(sub) {
  return Math.ceil((sub.expire * 1000 - Date.now()) / 86400000);
}

// subSummary: "осталось 143 ГБ, 23 дня".
function subSummary(sub) {
  if (!sub) return "";
  const parts = [];
  if (sub.hasUserInfo) {
    parts.push(sub.total ? `осталось ${fmtBytes(Math.max(0, sub.total - sub.upload - sub.download))}` : "безлимит");
  }
  if (sub.expire) {
    const d = daysLeft(sub);
    parts.push(d > 0 ? `${d} ${plural(d, "день", "дня", "дней")}` : "истекла");
  }
  return parts.join(", ");
}

function renderServers() {
  const list = $("#server-list");
  if (!state.servers.length) {
    list.innerHTML = `<div class="empty"><b>Добавьте первый сервер</b>Скопируйте ссылку на сервер или подписку от провайдера и нажмите «Вставить».
      <p class="empty-bot">Нет сервера? <button class="text-link" id="bot-link">Бесплатный VPN в @v4hood_bot</button></p></div>`;
    return;
  }
  const q = $("#search").value.trim().toLowerCase();
  const servers = state.servers.filter((s) => !q || s.name.toLowerCase().includes(q) || s.address.toLowerCase().includes(q));

  let html = q ? "" : autoRow();
  let lastGroup = null;
  for (const s of servers) {
    const group = s.subId || "";
    if (group !== lastGroup) {
      const sub = state.subscriptions.find((x) => x.id === s.subId);
      const sum = subSummary(sub);
      html += s.subId
        ? `<div class="group"><span title="${esc(subTitle(sub))}">${esc(subTitle(sub))}</span>${sum ? `<small>${esc(sum)}</small>` : ""}</div>`
        : `<div class="group"><span>Добавленные вручную</span></div>`;
      lastGroup = group;
    }
    html += `
      <div class="row ${s.id === state.selected ? (state.settings.autoBest ? "picked" : "selected") : ""}" data-id="${s.id}" tabindex="0">
        <div class="row-main">
          <div class="row-name" title="${esc(s.name)}">${esc(s.name)}</div>
          <div class="row-meta"><span class="proto">${esc(protoLine(s))}</span>${esc(s.address)}:${s.port}</div>
        </div>
        <div class="row-actions">
          ${s.ovpn?.needAuth ? `<button class="btn ghost small" data-act="login">Логин</button>` : ""}
          <button class="btn ghost small" data-act="copy">Копировать</button>
          <button class="btn ghost small" data-act="del">Удалить</button>
        </div>
        ${pingCell(s.id)}
      </div>`;
  }
  list.innerHTML = html || `<div class="empty"><b>Ничего не найдено</b>Попробуйте другое название или адрес.</div>`;
}

// autoRow: "the fastest server" as the first entry of the list. It picks by
// ping among the servers of the selected one's subscription.
function autoRow() {
  const sel = state.servers.find((s) => s.id === state.selected);
  if (!sel || state.servers.filter((s) => s.subId === sel.subId).length < 2) return "";
  const on = state.settings.autoBest;
  const where = sel.subId ? `подписки «${esc(subTitle(state.subscriptions.find((x) => x.id === sel.subId)))}»` : "добавленных вручную";
  const meta = on && state.status.state === "connected" ? `Сейчас ${esc(sel.name)}` : `По пингу среди серверов ${where}`;
  return `
    <div class="row auto ${on ? "selected" : ""}" data-auto="1" tabindex="0" role="switch" aria-checked="${on}">
      <svg class="auto-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M13 2 4 14h7l-1 8 9-12h-7z"/></svg>
      <div class="row-main">
        <div class="row-name">Самый быстрый сервер</div>
        <div class="row-meta">${meta}</div>
      </div>
      <span class="auto-state">${on ? "Включено" : ""}</span>
    </div>`;
}

// ---------- connection dock ----------

function selectedServer() {
  const st = state.status;
  return state.servers.find((s) => s.id === (st.state === "disconnected" ? state.selected : st.serverId));
}

let lastDockState = null;

function renderDock() {
  const st = state.status;
  const srv = selectedServer();
  const dock = $("#dock");
  if (lastDockState !== null && lastDockState !== st.state) {
    restart($("#status-word"), "changed");
    if (st.state === "connected") ripple();
  }
  lastDockState = st.state;
  dock.dataset.state = st.state;
  kick();

  $("#status-word").textContent = {
    connected: "Подключено", connecting: "Подключение…", disconnecting: "Отключение…", disconnected: "Отключено",
  }[st.state];
  $("#status-server").textContent = srv ? srv.name : "Сервер не выбран";
  $("#status-server").title = srv ? srv.name : "";

  let detail = "";
  if (st.state === "connected") {
    detail = st.mode === "tun" ? "Весь трафик идёт через VPN" : `Прокси 127.0.0.1:${state.settings.httpPort}`;
  } else if (srv?.subId) {
    detail = subSummary(state.subscriptions.find((x) => x.id === srv.subId));
    if (detail) detail = "Подписка: " + detail;
  }
  $("#status-detail").textContent = detail;
  $("#status-error").textContent =
    st.error === "need_admin" ? "Для режима «Весь трафик» нужны права администратора" :
    st.error === "need_ovpn_auth" ? "Введите логин и пароль OpenVPN" : st.error || "";

  const power = $("#power");
  power.textContent = { connected: "Отключить", connecting: "Подключение…", disconnecting: "Отключение…", disconnected: "Подключить" }[st.state];
  power.disabled = st.state === "connecting" || st.state === "disconnecting";
  $("#test-btn").disabled = st.state !== "connected";

  // WireGuard, AmneziaWG and OpenVPN are full tunnels: always "Весь трафик".
  const fullTunnel = !!(srv?.wg || srv?.ovpn);
  const mode = fullTunnel ? "tun" : state.settings.mode;
  $$("#mode-seg button").forEach((b) => {
    b.setAttribute("aria-checked", String(b.dataset.mode === mode));
    const locked = fullTunnel && b.dataset.mode === "proxy";
    b.disabled = locked;
    b.title = locked ? "WireGuard, AmneziaWG и OpenVPN работают только в режиме «Весь трафик»" : "";
  });
  $("#admin-proxy").hidden = fullTunnel;

  const foot = [upd?.current && upd.current !== "dev" ? "Версия " + bareVersion(upd.current) : "Версия для разработки"];
  if (st.admin && platform === "windows") foot.push("права администратора");
  $("#dock-foot").textContent = foot.join(", ");
  renderTotals();
}

// restart replays a CSS animation bound to cls.
function restart(el, cls) {
  el.classList.remove(cls);
  void el.offsetWidth;
  el.classList.add(cls);
}

// ripple: the connected colour spreads from the button.
function ripple() {
  const dock = $("#dock");
  const b = $("#power").getBoundingClientRect();
  const d = dock.getBoundingClientRect();
  dock.style.setProperty("--rx", `${b.left - d.left + b.width / 2}px`);
  dock.style.setProperty("--ry", `${b.top - d.top + dock.scrollTop + b.height / 2}px`);
  restart(dock, "just-connected");
  setTimeout(() => dock.classList.remove("just-connected"), 900);
}

// ---------- speed graph ----------

let traffic = null;
const HISTORY = 90; // samples kept by the client, one per second

// ---------- live speed: numbers and the graph behind the dock ----------
// Samples arrive once a second. Between them everything is animated frame
// by frame: the numbers roll towards the new speed, the curve morphs from
// its old shape to the new one while sliding one step left, and the scale
// follows the visible peak. With animations off it just redraws.

const GRAPH_TOP = 10; // the peak reaches this far from the top, in %
const live = {
  target: { up: 0, down: 0 }, // speeds of the last sample
  shown: { up: 0, down: 0 },  // speeds on screen
  from: [], to: [],           // the curve before and after the last sample
  at: 0,                      // when the last sample arrived
  scale: 0,
  raf: 0, last: 0,
};

function renderTraffic(t) {
  traffic = t;
  const next = soften((t.history || []).map((p) => p.down));
  // Morph from what is on screen, aligned to the new sample's positions.
  const cur = liveCurve(performance.now());
  let from = cur.length && next.length > cur.length ? cur : cur.slice(1);
  while (from.length < next.length) from.push(from.length ? from[from.length - 1] : 0);
  live.from = from.slice(-next.length);
  live.to = next;
  live.at = performance.now();
  live.target = { up: t.up || 0, down: t.down || 0 };
  renderTotals();
  kick();
}

const still = () => document.documentElement.classList.contains("no-anim");
const ease = (x) => 1 - Math.pow(1 - x, 3);

// liveCurve: the curve as it should look at time now.
function liveCurve(now) {
  const { from, to } = live;
  if (!to.length) return [];
  const k = still() ? 1 : ease(Math.min(1, (now - live.at) / 900));
  return to.map((v, i) => (from[i] ?? v) + (v - (from[i] ?? v)) * k);
}

function kick() {
  if (!live.raf && !document.hidden) live.raf = requestAnimationFrame(frame);
}

function frame(now) {
  live.raf = 0;
  if (document.hidden) return;
  const dt = Math.min(0.1, Math.max(0, (now - (live.last || now)) / 1000));
  live.last = now;
  const k = still() ? 1 : 1 - Math.exp(-dt / 0.22);
  let moving = false;
  for (const key of ["up", "down"]) {
    const d = live.target[key] - live.shown[key];
    live.shown[key] = Math.abs(d) < 1 ? live.target[key] : live.shown[key] + d * k;
    moving ||= live.shown[key] !== live.target[key];
  }
  $("#up").textContent = fmtSpeed(Math.round(live.shown.up));
  $("#down").textContent = fmtSpeed(Math.round(live.shown.down));
  const on = drawGraph(now, dt);
  // Keep animating while the curve slides or the numbers roll.
  if (!still() && (on || moving)) kick();
}

// soften evens out single-second spikes (weights 1-2-3-2-1).
function soften(v) {
  const at = (i) => v[Math.min(v.length - 1, Math.max(0, i))];
  return v.map((x, i) => (at(i - 2) + 2 * at(i - 1) + 3 * x + 2 * at(i + 1) + at(i + 2)) / 9);
}

// curve draws a smooth line through the points without overshooting
// (monotone cubic interpolation), so it never dips below zero.
function curve(pts) {
  const n = pts.length;
  if (n < 2) return "";
  const d = [], m = [];
  for (let i = 0; i < n - 1; i++) d.push((pts[i + 1][1] - pts[i][1]) / (pts[i + 1][0] - pts[i][0]));
  m.push(d[0]);
  for (let i = 1; i < n - 1; i++) m.push(d[i - 1] * d[i] <= 0 ? 0 : (d[i - 1] + d[i]) / 2);
  m.push(d[n - 2]);
  for (let i = 0; i < n - 1; i++) {
    if (d[i] === 0) { m[i] = m[i + 1] = 0; continue; }
    const a = m[i] / d[i], b = m[i + 1] / d[i], h = a * a + b * b;
    if (h > 9) { const t = 3 / Math.sqrt(h); m[i] = t * a * d[i]; m[i + 1] = t * b * d[i]; }
  }
  let path = `M${pts[0][0].toFixed(2)} ${pts[0][1].toFixed(2)}`;
  for (let i = 0; i < n - 1; i++) {
    const [x0, y0] = pts[i], [x1, y1] = pts[i + 1], dx = (x1 - x0) / 3;
    path += `C${(x0 + dx).toFixed(2)} ${(y0 + m[i] * dx).toFixed(2)} ${(x1 - dx).toFixed(2)} ${(y1 - m[i + 1] * dx).toFixed(2)} ${x1.toFixed(2)} ${y1.toFixed(2)}`;
  }
  return path;
}

// drawGraph draws the download curve; returns whether it is on screen.
function drawGraph(now, dt) {
  const g = $("#graph");
  const vals = liveCurve(now);
  const on = vals.length > 1 && state?.status.state === "connected";
  g.classList.toggle("live", on);
  if (vals.length < 2) {
    $("#graph-area").removeAttribute("d");
    $("#graph-down").removeAttribute("d");
    return false;
  }
  // The scale follows the visible peak: never below it (the curve stays
  // inside the dock), gently back down when the speed drops.
  const top = Math.max(64 * 1024, ...vals);
  const peak = top * 1.08;
  live.scale = !live.scale || still() ? peak : live.scale + (peak - live.scale) * (1 - Math.exp(-dt / (peak > live.scale ? 0.15 : 1.2)));
  live.scale = Math.max(live.scale, top);

  // The newest point slides in from one step past the right edge.
  const step = 100 / (HISTORY - 3);
  const phase = still() ? 1 : Math.min(1, (now - live.at) / 1000);
  const right = 100 + step * (1 - phase);
  const n = vals.length;
  const pts = vals.map((v, i) => [right - (n - 1 - i) * step, 100 - (v / live.scale) * (100 - GRAPH_TOP)]);
  const line = curve(pts);
  $("#graph-down").setAttribute("d", line);
  $("#graph-area").setAttribute("d", `${line}L${pts[n - 1][0].toFixed(2)} 101L${pts[0][0].toFixed(2)} 101Z`);
  return on;
}

function renderTotals() {
  const el = $("#totals");
  if (!traffic || !state) return;
  const session = traffic.sessionUp + traffic.sessionDown;
  const today = traffic.todayUp + traffic.todayDown;
  if (state.status.state === "connected") {
    el.textContent = `За сессию ${fmtBytes(session)}, сегодня ${fmtBytes(today)}`;
  } else {
    el.textContent = today ? `Сегодня через VPN ${fmtBytes(today)}` : "";
  }
}

// ---------- subscriptions ----------

function trafficBlock(sub) {
  if (!sub.hasUserInfo) return `<p class="sub-foot">Провайдер не сообщает расход трафика.</p>`;
  const used = sub.upload + sub.download;
  if (!sub.total) {
    return `<div class="traffic-line"><b>Безлимитный трафик</b><span class="muted">использовано ${fmtBytes(used)}</span></div>`;
  }
  const left = Math.max(0, sub.total - used);
  const pct = Math.min(100, (used / sub.total) * 100);
  const cls = pct >= 90 ? "bad" : pct >= 70 ? "mid" : "";
  return `
    <div class="traffic">
      <div class="traffic-line"><b>Осталось ${fmtBytes(left)}</b><span class="muted">из ${fmtBytes(sub.total)}</span></div>
      <div class="meter ${cls}"><span style="width:${pct.toFixed(1)}%"></span></div>
    </div>`;
}

function expireLine(sub) {
  if (!sub.expire) return sub.hasUserInfo ? "Бессрочно" : "";
  const date = new Date(sub.expire * 1000).toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric" });
  const d = daysLeft(sub);
  if (d <= 0) return `<span class="bad-text">Истекла ${date}</span>`;
  const text = `Действует до ${date}, ещё ${d} ${plural(d, "день", "дня", "дней")}`;
  return d <= 3 ? `<span class="bad-text">${text}</span>` : d <= 7 ? `<span class="warn">${text}</span>` : text;
}

function renderSubs() {
  const list = $("#sub-list");
  if (!state.subscriptions.length) {
    list.innerHTML = `<div class="empty"><b>Подписок пока нет</b>Вставьте ссылку на подписку от провайдера — серверы и остаток трафика будут обновляться сами.</div>`;
    return;
  }
  list.innerHTML = state.subscriptions.map((s) => {
    const count = state.servers.filter((x) => x.subId === s.id).length;
    const expire = expireLine(s);
    return `
      <article class="sub" data-id="${s.id}">
        <div class="sub-head">
          <div>
            <div class="sub-title">${esc(subTitle(s))}</div>
            <div class="sub-url">${esc(s.url)}</div>
          </div>
          <div class="sub-actions">
            ${s.webPageUrl ? `<button class="btn ghost small" data-act="open" data-url="${esc(s.webPageUrl)}">Сайт</button>` : ""}
            ${s.supportUrl ? `<button class="btn ghost small" data-act="open" data-url="${esc(s.supportUrl)}">Поддержка</button>` : ""}
            <button class="btn ghost small" data-act="update">Обновить</button>
            <button class="btn ghost small" data-act="rename">Переименовать</button>
            <button class="btn ghost small" data-act="del">Удалить</button>
          </div>
        </div>
        ${s.announce ? `<p class="announce">${esc(s.announce)}</p>` : ""}
        ${trafficBlock(s)}
        ${expire ? `<p>${expire}</p>` : ""}
        <p class="sub-foot">${count} ${plural(count, "сервер", "сервера", "серверов")}, обновлено ${s.updated ? fmtDate(s.updated) : "ещё не обновлялась"}, автообновление каждые ${s.updateInterval || 12} ч</p>
        ${s.lastError ? `<p class="sub-foot bad-text">Не удалось обновить: ${esc(s.lastError)}</p>` : ""}
      </article>`;
  }).join("");
}

// ---------- settings ----------

function fillSettings() {
  const f = $("#settings-form");
  const s = state.settings;
  f.mode.value = s.mode;
  f.socksPort.value = s.socksPort;
  f.httpPort.value = s.httpPort;
  f.dns.value = (s.dns || []).join(", ");
  f.bypassRu.checked = s.bypassRu;
  f.sound.checked = s.sound;
  f.autoConnect.checked = s.autoConnect;
  f.autoReconnect.checked = !s.noAutoReconnect;
  f.proxyRules.value = (s.proxyRules || []).join("\n");
  f.directRules.value = (s.directRules || []).join("\n");
  f.animations.checked = !s.noAnimations;
  f.updateCheck.checked = !s.noUpdateCheck;
  f.logLevel.value = s.logLevel;
  $$("#settings-form select").forEach(syncSelect);
  api().GetAutostart().then(renderAutostart);
  renderUpdate();
}

function readSettings() {
  const f = $("#settings-form");
  return {
    mode: f.mode.value,
    socksPort: Number(f.socksPort.value),
    httpPort: Number(f.httpPort.value),
    dns: f.dns.value.split(/[,\s]+/).filter(Boolean),
    bypassRu: f.bypassRu.checked,
    sound: f.sound.checked,
    autoConnect: f.autoConnect.checked,
    noAutoReconnect: !f.autoReconnect.checked,
    autoBest: state.settings.autoBest, // set from the server list
    proxyRules: f.proxyRules.value.split("\n"),
    directRules: f.directRules.value.split("\n"),
    noAnimations: !f.animations.checked,
    noUpdateCheck: !f.updateCheck.checked,
    logLevel: f.logLevel.value,
  };
}

function renderAutostart(st) {
  if (platform === "android") return; // see applyAndroid
  $("#autostart").checked = st.enabled;
  const hint = $("#autostart-hint");
  hint.classList.remove("warn");
  if (platform !== "windows") {
    // TUN rights are requested by the helper when connecting, not at login.
    hint.textContent = platform === "darwin"
      ? "Клиент будет запускаться при входе в систему свёрнутым в Dock."
      : "Клиент будет запускаться при входе в систему сразу в трей.";
    return;
  }
  if (!st.enabled) {
    hint.textContent = st.admin
      ? "Клиент будет запускаться сразу в трей с правами администратора — без окна UAC, все режимы работают."
      : "Клиент запущен без прав администратора, поэтому при автозапуске будет работать только режим «Прокси». Чтобы работал «Весь трафик», запустите Vlessia от имени администратора и включите автозапуск.";
  } else if (st.elevated) {
    hint.textContent = "Запускается сразу в трей с правами администратора, все режимы работают.";
  } else {
    hint.textContent = "Запускается без прав администратора: «Весь трафик», WireGuard, AmneziaWG и OpenVPN не подключатся сами. Запустите Vlessia от имени администратора и включите автозапуск заново.";
    hint.classList.add("warn");
  }
}

// ---------- updates ----------

function renderUpdate() {
  if (!upd) return;
  const banner = $("#update-banner");
  const busy = upd.state === "downloading" || upd.state === "installing";
  banner.hidden = !(upd.state === "available" || busy);
  if (!banner.hidden) {
    const v = bareVersion(upd.latest);
    $("#update-text").textContent =
      upd.state === "downloading" ? `Загружаю версию ${v}…` :
      upd.state === "installing" ? `Устанавливаю версию ${v}, клиент перезапустится` :
      `Доступна версия ${v}`;
    $("#update-bar").hidden = !busy;
    $("#update-progress").style.width = Math.round((upd.progress || 0) * 100) + "%";
    banner.querySelector(".update-actions").hidden = busy;
    $("#update-go").textContent = upd.installed ? "Обновить" : "Скачать";
  }

  const cur = upd.current === "dev" ? "Версия для разработки" : `Установлена версия ${bareVersion(upd.current)}.`;
  const line = {
    checking: "Проверяю…",
    latest: "Это последняя версия.",
    available: `Доступна версия ${bareVersion(upd.latest)}.`,
    downloading: "Загружаю обновление…",
    installing: "Устанавливаю обновление…",
    error: upd.error,
  }[upd.state] || "";
  $("#version-line").textContent = line ? `${cur} ${line}` : cur;
  $("#check-update").disabled = upd.state === "checking" || busy;
}

async function applyUpdate() {
  $("#update-go").disabled = true;
  try {
    await call("ApplyUpdate");
  } catch { /* toast shown */ } finally {
    $("#update-go").disabled = false;
  }
}

// ---------- navigation & misc ----------

async function refreshLogs() {
  if (logsBusy) return;
  logsBusy = true;
  try {
    const pre = $("#logs");
    const atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 20;
    pre.textContent = await api().GetLogs();
    if (atBottom) pre.scrollTop = pre.scrollHeight;
  } finally {
    logsBusy = false;
  }
}

function toggleAuto() {
  const on = !state.settings.autoBest;
  call("SetAutoBest", on).then(() => {
    if (on) toast(state.status.state === "connected" ? "Переподключаюсь к самому быстрому серверу" : "Перед подключением выберу самый быстрый сервер");
  }).catch(() => {});
}

// Disclosures (<details class="more">) open and close smoothly.
function animateDetails(details) {
  const summary = details.querySelector("summary");
  const body = details.querySelector(".more-body");
  let anim = null;
  summary.addEventListener("click", (e) => {
    if (document.documentElement.classList.contains("no-anim")) return;
    e.preventDefault();
    anim?.cancel();
    const opening = !details.open;
    if (opening) details.open = true;
    const full = body.scrollHeight;
    const frames = [{ height: "0px", opacity: 0 }, { height: `${full}px`, opacity: 1 }];
    anim = body.animate(opening ? frames : frames.reverse(), { duration: 240, easing: "cubic-bezier(.2,.8,.2,1)" });
    // Finish by a timer too: animations stall while the window is hidden.
    const done = () => {
      if (!opening) details.open = false;
      anim = null;
    };
    const timer = setTimeout(done, 300);
    anim.onfinish = () => {
      clearTimeout(timer);
      done();
    };
    anim.oncancel = () => clearTimeout(timer);
  });
}

// The underline slides to the active tab.
function moveTabMark() {
  const tab = $(".tab.active");
  const tabs = $(".tabs");
  if (!tab) return;
  tabs.style.setProperty("--tab-x", `${tab.offsetLeft}px`);
  tabs.style.setProperty("--tab-w", `${tab.offsetWidth}px`);
}

// Animations follow the setting and the system's "reduce motion".
function applyMotion(off) {
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  document.documentElement.classList.toggle("no-anim", off || reduce);
}

function showView(name) {
  $$(".tab").forEach((b) => b.classList.toggle("active", b.dataset.view === name));
  moveTabMark();
  $$(".view").forEach((v) => v.classList.toggle("active", v.id === "view-" + name));
  clearInterval(logsTimer);
  if (name === "settings") fillSettings();
  if (name === "logs") {
    refreshLogs();
    logsTimer = setInterval(() => !document.hidden && $("#logs-auto").checked && refreshLogs(), 2000);
  }
}

// askCredentials asks for an OpenVPN login; connect=true connects after.
function askCredentials(id, connect) {
  const srv = state.servers.find((s) => s.id === id);
  if (!srv) return;
  const d = $("#auth-dialog");
  d.dataset.id = id;
  d.dataset.connect = connect ? "1" : "";
  $("#auth-server").textContent = srv.name;
  $("#auth-user").value = srv.ovpn?.username || "";
  $("#auth-pass").value = "";
  d.showModal();
}

const added = (n) => toast(`${n} ${plural(n, "сервер добавлен", "сервера добавлено", "серверов добавлено")}`);

function render() {
  if (!state) return;
  renderDock();
  renderServers();
  renderSubs();
}

// ---------- dropdowns ----------
// The system draws a <select>'s list itself, so it cannot be animated or
// styled. Each select gets a button and an animated list of its own; the
// hidden select keeps the value, so forms read it as before.

function enhanceSelect(sel) {
  // Inside a <label>, clicks on the list would go to the hidden select.
  const lab = sel.closest("label");
  if (lab) {
    const div = document.createElement("div");
    div.className = lab.className;
    div.append(...lab.childNodes);
    lab.replaceWith(div);
  }
  const box = document.createElement("div");
  box.className = "dropdown";
  box.innerHTML = `
    <button type="button" class="dropdown-btn" aria-haspopup="listbox" aria-expanded="false">
      <span class="dropdown-label"></span>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>
    </button>
    <ul class="dropdown-list" role="listbox" tabindex="-1" hidden></ul>`;
  sel.hidden = true;
  sel.after(box);
  box.prepend(sel);
  const btn = box.querySelector(".dropdown-btn");
  const list = box.querySelector(".dropdown-list");
  const label = box.parentElement.querySelector(":scope > span")?.textContent;
  if (label) btn.setAttribute("aria-label", label);

  const items = () => [...list.children];
  const close = (focus) => {
    if (list.hidden) return;
    list.hidden = true;
    box.classList.remove("open", "up");
    btn.setAttribute("aria-expanded", "false");
    if (focus) btn.focus();
  };
  const choose = (value) => {
    if (sel.value !== value) {
      sel.value = value;
      sel.dispatchEvent(new Event("change", { bubbles: true }));
    }
    syncSelect(sel);
    close(true);
  };
  const open = () => {
    list.innerHTML = [...sel.options].map((o) =>
      `<li role="option" data-value="${esc(o.value)}" aria-selected="${o.value === sel.value}" tabindex="-1">${esc(o.textContent)}</li>`).join("");
    list.hidden = false;
    box.classList.add("open");
    btn.setAttribute("aria-expanded", "true");
    // Open upwards when the list would leave the scrolling area.
    const area = box.closest(".settings") || document.body;
    const room = area.getBoundingClientRect().bottom - btn.getBoundingClientRect().bottom;
    box.classList.toggle("up", room < list.offsetHeight + 12);
    (list.querySelector('[aria-selected="true"]') || list.firstElementChild)?.focus();
  };

  btn.addEventListener("click", () => (list.hidden ? open() : close(true)));
  btn.addEventListener("keydown", (e) => {
    if (["ArrowDown", "ArrowUp", "Enter", " "].includes(e.key)) {
      e.preventDefault();
      open();
    }
  });
  list.addEventListener("click", (e) => {
    const li = e.target.closest("li");
    if (li) choose(li.dataset.value);
  });
  list.addEventListener("keydown", (e) => {
    const all = items();
    const i = all.indexOf(document.activeElement);
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      all[Math.min(all.length - 1, Math.max(0, i + (e.key === "ArrowDown" ? 1 : -1)))]?.focus();
    } else if (e.key === "Home" || e.key === "End") {
      e.preventDefault();
      all[e.key === "Home" ? 0 : all.length - 1]?.focus();
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (i >= 0) choose(all[i].dataset.value);
    } else if (e.key === "Escape") {
      e.preventDefault();
      close(true);
    } else if (e.key === "Tab") {
      close(false);
    }
  });
  document.addEventListener("pointerdown", (e) => {
    if (!box.contains(e.target)) close(false);
  });
  syncSelect(sel);
}

// syncSelect shows the select's current choice on its button.
function syncSelect(sel) {
  const label = sel.closest(".dropdown")?.querySelector(".dropdown-label");
  if (label) label.textContent = sel.selectedOptions[0]?.textContent ?? "";
}

function bind() {
  $$("#settings-form select").forEach(enhanceSelect);
  $$("details.more").forEach(animateDetails);
  $$(".tab").forEach((b) => b.addEventListener("click", () => showView(b.dataset.view)));

  $("#power").addEventListener("click", () => call("Toggle").catch(() => {}));
  $$("#mode-seg button").forEach((b) => b.addEventListener("click", () => call("SetMode", b.dataset.mode).catch(() => {})));

  $("#test-btn").addEventListener("click", async () => {
    const btn = $("#test-btn");
    btn.textContent = "Проверяю…";
    try {
      const ms = await call("TestConnection");
      toast(`Соединение работает, задержка ${ms} мс`);
    } catch { /* toast shown */ } finally {
      btn.textContent = "Проверить соединение";
    }
  });

  $("#paste-btn").addEventListener("click", () => call("ImportClipboard").then(added).catch(() => {}));
  $("#qr-btn").addEventListener("click", () => call("ScanQR").then((n) => n && added(n)).catch(() => {}));
  $("#welcome-dialog").addEventListener("close", () => {
    if ($("#welcome-dialog").returnValue === "bot") api().OpenURL(BOT_URL);
  });
  $("#add-btn").addEventListener("click", () => {
    $("#add-text").value = "";
    $("#add-dialog").showModal();
  });
  $("#add-dialog").addEventListener("close", () => {
    const v = $("#add-dialog").returnValue;
    if (v === "ok" && $("#add-text").value.trim()) call("ImportText", $("#add-text").value).then(added).catch(() => {});
    if (v === "file") call("ImportFile").then((n) => n && added(n)).catch(() => {});
  });

  $("#admin-dialog").addEventListener("close", () => {
    const v = $("#admin-dialog").returnValue;
    if (v === "restart") call("RestartAsAdmin").catch(() => {});
    if (v === "proxy") call("SetMode", "proxy").then(() => call("Connect")).catch(() => {});
  });

  $("#ping-btn").addEventListener("click", () => {
    for (const s of state.servers) pings[s.id] = null;
    renderServers();
    api().PingAll();
  });
  $("#update-subs-btn").addEventListener("click", () => {
    if (!state.subscriptions.length) return toast("Подписок пока нет — добавьте их на вкладке «Подписки»", true);
    call("UpdateAllSubscriptions").then(() => toast("Подписки обновлены")).catch(() => {});
  });
  $("#search").addEventListener("input", renderServers);

  const list = $("#server-list");
  list.addEventListener("click", (e) => {
    if (e.target.closest("#bot-link")) return api().OpenURL(BOT_URL);
    const row = e.target.closest(".row");
    if (!row) return;
    if (row.dataset.auto) return toggleAuto();
    const id = row.dataset.id;
    const act = e.target.closest("[data-act]")?.dataset.act;
    if (act === "copy") {
      const isConf = !state.servers.find((x) => x.id === id)?.link.includes("://");
      return call("CopyLink", id).then(() => toast(isConf ? "Конфигурация скопирована" : "Ссылка скопирована")).catch(() => {});
    }
    if (act === "del") return call("DeleteServer", id).then(() => toast("Сервер удалён")).catch(() => {});
    if (act === "login") return askCredentials(id, false);
    if (state.settings.autoBest) call("SetAutoBest", false).then(() => call("SelectServer", id)).catch(() => {});
    else if (id !== state.selected) call("SelectServer", id).catch(() => {});
  });
  list.addEventListener("dblclick", (e) => {
    const row = e.target.closest(".row");
    if (row && !e.target.closest("[data-act]") && state.status.state === "disconnected") {
      if (row.dataset.auto) return call("SetAutoBest", true).then(() => call("Connect")).catch(() => {});
      call("SelectServer", row.dataset.id).then(() => call("Connect")).catch(() => {});
    }
  });
  list.addEventListener("keydown", (e) => {
    const row = e.target.closest(".row");
    if (row && e.key === "Enter") {
      if (row.dataset.auto) toggleAuto();
      else call("SelectServer", row.dataset.id).catch(() => {});
    }
  });

  $("#sub-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const url = $("#sub-url").value.trim();
    if (!url) return;
    const btn = $("#sub-add");
    btn.disabled = true;
    try {
      const n = await call("AddSubscription", $("#sub-name").value.trim(), url);
      toast(`Подписка добавлена: ${n} ${plural(n, "сервер", "сервера", "серверов")}`);
      $("#sub-url").value = "";
      $("#sub-name").value = "";
    } catch { /* toast shown */ } finally {
      btn.disabled = false;
    }
  });

  $("#sub-list").addEventListener("click", async (e) => {
    const btn = e.target.closest("[data-act]");
    const id = e.target.closest(".sub")?.dataset.id;
    if (!btn || !id) return;
    const act = btn.dataset.act;
    if (act === "open") return api().OpenURL(btn.dataset.url);
    if (act === "del") return call("DeleteSubscription", id).then(() => toast("Подписка удалена")).catch(() => {});
    if (act === "rename") {
      const sub = state.subscriptions.find((s) => s.id === id);
      $("#rename-input").value = sub.name || "";
      $("#rename-input").placeholder = sub.title || "Название";
      $("#rename-dialog").dataset.id = id;
      return $("#rename-dialog").showModal();
    }
    btn.disabled = true;
    try {
      const n = await call("UpdateSubscription", id);
      toast(`Подписка обновлена: ${n} ${plural(n, "сервер", "сервера", "серверов")}`);
    } catch { /* toast shown */ } finally {
      btn.disabled = false;
    }
  });

  $("#auth-dialog").addEventListener("close", () => {
    const d = $("#auth-dialog");
    if (d.returnValue !== "ok") return;
    const p = call("SetOpenVPNCredentials", d.dataset.id, $("#auth-user").value, $("#auth-pass").value);
    if (d.dataset.connect === "1") p.then(() => call("Connect")).catch(() => {});
    else p.then(() => toast("Логин сохранён")).catch(() => {});
  });
  $("#rename-dialog").addEventListener("close", () => {
    const d = $("#rename-dialog");
    if (d.returnValue === "ok") call("RenameSubscription", d.dataset.id, $("#rename-input").value).catch(() => {});
  });

  // Settings save as soon as a field changes.
  $("#settings-form").addEventListener("submit", (e) => e.preventDefault());
  $("#settings-form").addEventListener("change", (e) => {
    if (e.target.id === "autostart") return;
    if (e.target.name === "animations") applyMotion(!e.target.checked);
    // A bad rule keeps the text so it can be corrected.
    const keep = e.target.tagName === "TEXTAREA";
    call("SaveSettings", readSettings()).then(() => toast("Сохранено")).catch(() => keep || fillSettings());
  });
  $("#autostart").addEventListener("change", (e) =>
    call("SetAutostart", e.target.checked).then(renderAutostart).catch(() => api().GetAutostart().then(renderAutostart)));
  $("#open-dir").addEventListener("click", () => api().OpenDataDir());
  $("#logs-refresh").addEventListener("click", refreshLogs);

  $("#check-update").addEventListener("click", () => call("CheckUpdate").catch(() => {}));
  $("#update-go").addEventListener("click", applyUpdate);
  $("#update-notes").addEventListener("click", () => {
    $("#notes-title").textContent = `Что нового в версии ${bareVersion(upd.latest)}`;
    $("#notes-body").textContent = upd.notes || "Описание изменений не опубликовано.";
    $("#notes-dialog").showModal();
  });
  $("#notes-dialog").addEventListener("close", () => {
    if ($("#notes-dialog").returnValue === "update") applyUpdate();
  });

  window.runtime.EventsOn("state", (s) => {
    const prevError = state?.status.error;
    state = s;
    render();
    if (s.status.error === "need_admin" && prevError !== "need_admin" && !$("#admin-dialog").open) $("#admin-dialog").showModal();
    if (s.status.error === "need_ovpn_auth" && prevError !== "need_ovpn_auth" && !$("#auth-dialog").open) askCredentials(s.status.serverId, true);
  });
  window.runtime.EventsOn("traffic", renderTraffic);
  window.runtime.EventsOn("notice", (text) => toast(text));
  window.addEventListener("resize", moveTabMark);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      live.last = performance.now();
      kick();
      if ($("#view-logs").classList.contains("active") && $("#logs-auto").checked) refreshLogs();
    }
  });
  window.runtime.EventsOn("ping", (p) => {
    pings[p.id] = p.ms;
    const cell = document.querySelector(`#server-list .row[data-id="${p.id}"] .ping`);
    if (cell) {
      cell.outerHTML = pingCell(p.id);
      document.querySelector(`#server-list .row[data-id="${p.id}"] .ping`)?.classList.add("fresh");
    }
  });
  window.runtime.EventsOn("update", (u) => {
    upd = u;
    renderUpdate();
    if (state) renderDock();
  });
}

// Wording that differs by operating system.
function applyPlatform() {
  if (platform === "android") return applyAndroid();
  if (platform === "windows") return;
  const tunOption = $('#settings-form select[name="mode"] option[value="tun"]');
  tunOption.textContent = "Весь трафик — при подключении система попросит пароль администратора";
}

// Android always routes all traffic through the VPN and starts with the
// system (always-on VPN), so the mode and autostart controls give way.
function applyAndroid() {
  $("#mode-seg").hidden = true;
  $("#qr-btn").title = "Сканировать QR-код сервера или подписки";
  $('#settings-form select[name="mode"]').closest(".row-field").hidden = true;
  $("#autostart").closest("label").hidden = true;
  $("#autostart-hint").textContent =
    "Чтобы VPN включался сам после перезагрузки телефона, включите для Vlessia «Постоянный VPN» в настройках Android.";
  const vpnSettings = document.createElement("div");
  vpnSettings.className = "inline";
  vpnSettings.innerHTML = `<button class="btn ghost" type="button">Открыть настройки VPN</button>`;
  vpnSettings.firstChild.addEventListener("click", () => api().OpenVpnSettings());
  $("#autostart-hint").after(vpnSettings);
  $('#settings-form input[name="autoConnect"]').parentElement.lastChild.textContent =
    " Подключаться к выбранному серверу при открытии приложения";
  $("#open-dir").closest(".inline").hidden = true;
  $("#dns-hint").textContent = "Через запятую. Первый также назначается VPN-подключению Android.";
}

async function init() {
  bind();
  let t;
  [state, upd, platform, t] = await Promise.all([api().GetState(), api().GetUpdate(), api().GetPlatform(), api().GetTraffic()]);
  applyMotion(state.settings.noAnimations);
  applyPlatform();
  moveTabMark();
  renderTraffic(t);
  document.fonts.ready.then(moveTabMark);
  render();
  renderUpdate();
  welcome();
}

// First run: offer a free VPN from the Telegram bot, once.
async function welcome() {
  if (!(await api().WelcomePending())) return;
  $("#welcome-dialog").showModal();
  api().WelcomeSeen();
}

// Wails injects its runtime asynchronously; wait until bindings are ready.
(function waitForRuntime() {
  if (window.go?.main?.App && window.runtime) init();
  else setTimeout(waitForRuntime, 20);
})();
