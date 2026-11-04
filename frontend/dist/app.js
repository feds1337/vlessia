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
// Android: the apps picked for the tunnel, and the list of installed ones
// (fetched from the native side on first use).
let androidApps = [];
let installedApps = null;
// Selects that were turned into animated dropdowns (they re-sync on a
// language switch).
const enhancedSelects = [];

// ---------- helpers ----------

function toast(msg, isError = false) {
  const el = document.createElement("div");
  el.className = "toast" + (isError ? " error" : "");
  el.textContent = trGo(msg);
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

// fmtSpeed, fmtBytes, fmtDate, fmtDateFull, plural and unitPlural come
// from i18n.js.

const bareVersion = (v) => String(v || "").replace(/^v/, "");

// The Telegram bot offered on first run and when there are no servers.
const BOT_URL = "https://t.me/v4hood_bot";

const isFullTunnel = (s) => !!(s?.wg || s?.ovpn);

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
  if (ms === -2) return `<span class="ping" title="${esc(t("ping.na_title"))}">—</span>`;
  if (ms < 0) return `<span class="ping bad">${esc(t("ping.none"))}</span>`;
  const cls = ms < 150 ? "good" : ms < 400 ? "mid" : "bad";
  return `<span class="ping ${cls}">${esc(t("ping.ms", ms))}</span>`;
}

function subTitle(sub) {
  return sub?.name || sub?.title || t("sub.fallback");
}

function daysLeft(sub) {
  return Math.ceil((sub.expire * 1000 - Date.now()) / 86400000);
}

// subSummary: "осталось 143 ГБ, 23 дня".
function subSummary(sub) {
  if (!sub) return "";
  const parts = [];
  if (sub.hasUserInfo) {
    parts.push(sub.total ? t("sub.left", fmtBytes(Math.max(0, sub.total - sub.upload - sub.download))) : t("sub.unlimited"));
  }
  if (sub.expire) {
    const d = daysLeft(sub);
    parts.push(d > 0 ? `${d} ${unitPlural("unit.day", d)}` : t("sub.expired"));
  }
  return parts.join(", ");
}

function renderServers() {
  const list = $("#server-list");
  if (!state.servers.length) {
    list.innerHTML = `<div class="empty"><b>${esc(t("servers.empty"))}</b>${esc(t("servers.empty_sub"))}
      <p class="empty-bot"><button class="text-link" id="bot-link">${esc(t("servers.free_bot"))}</button></p></div>`;
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
        : `<div class="group"><span>${esc(t("servers.manual"))}</span></div>`;
      lastGroup = group;
    }
    const relay = s.via ? state.servers.find((x) => x.id === s.via) : null;
    html += `
      <div class="row ${s.id === state.selected ? (state.settings.autoBest ? "picked" : "selected") : ""}" data-id="${s.id}" tabindex="0">
        <div class="row-main">
          <div class="row-name" title="${esc(s.name + " — " + s.address + ":" + s.port)}">${esc(s.name)}</div>
          <div class="row-meta"><span class="proto">${esc(protoLine(s))}</span>${relay ? `<span class="via-mini">· ${esc(t("servers.via_set", relay.name))}</span>` : ""}</div>
        </div>
        <div class="row-actions">
          ${s.ovpn?.needAuth ? `<button class="btn ghost small" data-act="login">${esc(t("servers.login"))}</button>` : ""}
          <button class="btn ghost small" data-act="via" title="${relay ? esc(t("servers.via_set", relay.name)) : esc(t("d.via_title"))}">${esc(t("servers.via"))}</button>
          <button class="btn ghost small" data-act="copy">${esc(t("servers.copy"))}</button>
          ${s.subId ? "" : `<button class="btn ghost small" data-act="del">${esc(t("servers.del"))}</button>`}
        </div>
        ${pingCell(s.id)}
      </div>`;
  }
  list.innerHTML = html || `<div class="empty"><b>${esc(t("servers.not_found"))}</b>${esc(t("servers.not_found_sub"))}</div>`;
}

// autoRow: "the fastest server" as the first entry of the list. It picks by
// ping among the servers of the selected one's subscription.
function autoRow() {
  const sel = state.servers.find((s) => s.id === state.selected);
  if (!sel || state.servers.filter((s) => s.subId === sel.subId).length < 2) return "";
  const on = state.settings.autoBest;
  const where = sel.subId
    ? t("auto.meta_sub", subTitle(state.subscriptions.find((x) => x.id === sel.subId)))
    : t("auto.meta_manual");
  const meta = on && state.status.state === "connected" ? t("auto.now", sel.name) : where;
  return `
    <div class="row auto ${on ? "selected" : ""}" data-auto="1" tabindex="0" role="switch" aria-checked="${on}">
      <svg class="auto-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M13 2 4 14h7l-1 8 9-12h-7z"/></svg>
      <div class="row-main">
        <div class="row-name">${esc(t("auto.title"))}</div>
        <div class="row-meta">${esc(meta)}</div>
      </div>
      <span class="auto-state">${on ? esc(t("auto.on")) : ""}</span>
    </div>`;
}

// ---------- connection dock ----------

function selectedServer() {
  const st = state.status;
  const id = st.state === "disconnected" ? state.selected : st.serverId;
  // The status keeps the id of the connection; after a subscription refresh
  // the servers get new ids, so fall back to the current selection instead
  // of showing "no server" while connected.
  return state.servers.find((s) => s.id === id) || state.servers.find((s) => s.id === state.selected) || null;
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
    connected: t("status.connected"), connecting: t("status.connecting"),
    disconnecting: t("status.disconnecting"), disconnected: t("status.disconnected"),
  }[st.state] || t("status.disconnected");
  $("#status-server").textContent = srv ? srv.name : t("dock.no_server");
  $("#status-server").title = srv ? srv.name : "";

  let detail = "";
  if (st.state === "connected") {
    // No "Proxy 127.0.0.1:10809" line: the mode is already in the segment
    // above, and the port only clutters the dock.
    const parts = [];
    if (st.mode === "tun") parts.push(t("dock.detail_tun"));
    const relay = srv?.via ? state.servers.find((x) => x.id === srv.via) : null;
    if (relay) parts.push(t("dock.detail_via", relay.name));
    if (state.settings.balance && srv && !isFullTunnel(srv)) {
      const n = state.servers.filter((s) => s.subId === srv.subId && !isFullTunnel(s)).length;
      if (n >= 2) parts.push(t("dock.detail_balance", n, unitPlural("unit.server", n)));
    }
    detail = parts.join(" · ");
  } else if (srv?.subId) {
    detail = subSummary(state.subscriptions.find((x) => x.id === srv.subId));
    if (detail) detail = t("dock.detail_sub", detail);
  }
  $("#status-detail").textContent = detail;
  $("#status-error").textContent = st.kill
    ? t("dock.kill")
    : st.error === "need_admin" ? t("dock.err_need_admin")
    : st.error === "need_ovpn_auth" ? t("dock.err_need_auth")
    : trGo(st.error || "");

  const power = $("#power");
  power.textContent = {
    connected: t("dock.disconnect"), connecting: t("status.connecting"),
    disconnecting: t("status.disconnecting"), disconnected: t("dock.connect"),
  }[st.state] || t("dock.connect");
  power.disabled = st.state === "connecting" || st.state === "disconnecting";
  $("#test-btn").disabled = st.state !== "connected";

  // WireGuard, AmneziaWG and OpenVPN are full tunnels: always "Весь трафик".
  const fullTunnel = isFullTunnel(srv);
  const mode = fullTunnel ? "tun" : state.settings.mode;
  $$("#mode-seg button").forEach((b) => {
    b.setAttribute("aria-checked", String(b.dataset.mode === mode));
    const locked = fullTunnel && b.dataset.mode === "proxy";
    b.disabled = locked;
    b.title = locked ? t("dock.tun_locked") : "";
  });
  $("#admin-proxy").hidden = fullTunnel;

  const foot = [upd?.current && upd.current !== "dev" ? t("dock.version", bareVersion(upd.current)) : t("dock.version_dev")];
  if (st.admin && platform === "windows") foot.push(t("dock.admin"));
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
    el.textContent = t("dock.session", fmtBytes(session), fmtBytes(today));
  } else {
    el.textContent = today ? t("dock.today", fmtBytes(today)) : "";
  }
}

// ---------- subscriptions ----------

function trafficBlock(sub) {
  if (!sub.hasUserInfo) return `<p class="sub-foot">${esc(t("subs.traffic_none"))}</p>`;
  const used = sub.upload + sub.download;
  if (!sub.total) {
    return `<div class="traffic-line"><b>${esc(t("subs.traffic_unl"))}</b><span class="muted">${esc(t("subs.used", fmtBytes(used)))}</span></div>`;
  }
  const left = Math.max(0, sub.total - used);
  const pct = Math.min(100, (used / sub.total) * 100);
  const cls = pct >= 90 ? "bad" : pct >= 70 ? "mid" : "";
  return `
    <div class="traffic">
      <div class="traffic-line"><b>${esc(t("subs.left", fmtBytes(left)))}</b><span class="muted">${esc(t("subs.of", fmtBytes(sub.total)))}</span></div>
      <div class="meter ${cls}"><span style="width:${pct.toFixed(1)}%"></span></div>
    </div>`;
}

function expireLine(sub) {
  if (!sub.expire) return sub.hasUserInfo ? t("subs.forever") : "";
  const date = fmtDateFull(sub.expire);
  const d = daysLeft(sub);
  if (d <= 0) return `<span class="bad-text">${esc(t("subs.expired_on", date))}</span>`;
  const text = esc(t("subs.valid", date, d, unitPlural("unit.day", d)));
  return d <= 3 ? `<span class="bad-text">${text}</span>` : d <= 7 ? `<span class="warn">${text}</span>` : text;
}

function renderSubs() {
  const list = $("#sub-list");
  if (!state.subscriptions.length) {
    list.innerHTML = `<div class="empty"><b>${esc(t("subs.empty"))}</b>${esc(t("subs.empty_sub"))}</div>`;
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
            ${s.webPageUrl ? `<button class="btn ghost small" data-act="open" data-url="${esc(s.webPageUrl)}">${esc(t("subs.site"))}</button>` : ""}
            ${s.supportUrl ? `<button class="btn ghost small" data-act="open" data-url="${esc(s.supportUrl)}">${esc(t("subs.support"))}</button>` : ""}
            <button class="btn ghost small" data-act="update">${esc(t("subs.update"))}</button>
            <button class="btn ghost small" data-act="rename">${esc(t("subs.rename"))}</button>
            <button class="btn ghost small" data-act="del">${esc(t("servers.del"))}</button>
          </div>
        </div>
        ${s.announce ? `<p class="announce">${esc(s.announce)}</p>` : ""}
        ${trafficBlock(s)}
        ${expire ? `<p>${expire}</p>` : ""}
        <p class="sub-foot">${esc(t("subs.foot", count, unitPlural("unit.server", count), s.updated ? fmtDate(s.updated) : t("subs.never"), s.updateInterval || 12))}</p>
        ${s.lastError ? `<p class="sub-foot bad-text">${esc(t("subs.err", trGo(s.lastError)))}</p>` : ""}
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
  f.killSwitch.checked = s.killSwitch;
  f.balance.checked = s.balance;
  f.balanceStrategy.value = s.balanceStrategy || "";
  $("#balance-strategy").hidden = !s.balance;
  f.proxyRules.value = (s.proxyRules || []).join("\n");
  f.directRules.value = (s.directRules || []).join("\n");
  f.proxyApps.value = (s.proxyApps || []).join("\n");
  f.directApps.value = (s.directApps || []).join("\n");
  if (platform === "android") {
    f.androidAppsMode.value = s.androidAppsMode || "";
    androidApps = (s.androidApps || []).slice();
    renderAppsCount();
  }
  f.animations.checked = !s.noAnimations;
  f.theme.value = s.theme || "";
  f.accent.value = s.accent || "";
  f.updateCheck.checked = !s.noUpdateCheck;
  f.logLevel.value = s.logLevel;
  $("#lang-select").value = lang;
  $("#kill-hint").textContent = t(platform === "android" ? "s.kill_hint_android" : "s.kill_hint");
  $$("#settings-form select").forEach(syncSelect);
  api().GetAutostart().then(renderAutostart);
  renderUpdate();
}

function readSettings() {
  const f = $("#settings-form");
  const android = platform === "android";
  return {
    mode: f.mode.value,
    socksPort: Number(f.socksPort.value),
    httpPort: Number(f.httpPort.value),
    dns: f.dns.value.split(/[,\s]+/).filter(Boolean),
    bypassRu: f.bypassRu.checked,
    sound: f.sound.checked,
    autoConnect: f.autoConnect.checked,
    noAutoReconnect: !f.autoReconnect.checked,
    killSwitch: f.killSwitch.checked,
    balance: f.balance.checked,
    balanceStrategy: f.balanceStrategy.value,
    autoBest: state.settings.autoBest, // set from the server list
    proxyRules: f.proxyRules.value.split("\n"),
    directRules: f.directRules.value.split("\n"),
    proxyApps: android ? state.settings.proxyApps || [] : f.proxyApps.value.split("\n"),
    directApps: android ? state.settings.directApps || [] : f.directApps.value.split("\n"),
    androidApps: android ? androidApps : state.settings.androidApps || [],
    androidAppsMode: android ? f.androidAppsMode.value : state.settings.androidAppsMode || "",
    noAnimations: !f.animations.checked,
    theme: f.theme.value,
    accent: f.accent.value,
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
    hint.textContent = platform === "darwin" ? t("s.autostart_h_dock") : t("s.autostart_h_tray");
    return;
  }
  if (!st.enabled) {
    hint.textContent = st.admin ? t("s.autostart_h_admin_off") : t("s.autostart_h_no_admin");
  } else if (st.elevated) {
    hint.textContent = t("s.autostart_h_elevated");
  } else {
    hint.textContent = t("s.autostart_h_degraded");
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
      upd.state === "downloading" ? t("upd.download", v) :
      upd.state === "installing" ? t("upd.install", v) :
      t("upd.available", v);
    $("#update-bar").hidden = !busy;
    $("#update-progress").style.width = Math.round((upd.progress || 0) * 100) + "%";
    banner.querySelector(".update-actions").hidden = busy;
    $("#update-go").textContent = upd.installed ? t("dock.update") : t("dock.download");
  }

  const cur = upd.current === "dev" ? t("dock.version_dev") : t("upd.installed", bareVersion(upd.current));
  const line = {
    checking: t("upd.checking"),
    latest: t("upd.latest"),
    available: t("upd.available", bareVersion(upd.latest)),
    downloading: t("upd.dl"),
    installing: t("upd.inst"),
    error: trGo(upd.error),
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
    if (on) toast(state.status.state === "connected" ? t("toast.auto_on_conn") : t("toast.auto_on"));
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

// openViaDialog picks the relay of a multi-tunnel chain.
function openViaDialog(id) {
  const srv = state.servers.find((s) => s.id === id);
  if (!srv) return;
  const sel = $("#via-select");
  sel.innerHTML = [`<option value="">${esc(t("d.via_none"))}</option>`]
    .concat(state.servers.filter((s) => s.id !== id)
      .map((s) => `<option value="${s.id}">${esc(s.name + " — " + protoLine(s))}</option>`))
    .join("");
  sel.value = srv.via || "";
  $("#via-sub").textContent = t("d.via_sub", srv.name, srv.address);
  const d = $("#via-dialog");
  d.dataset.id = id;
  d.showModal();
}

// ---------- android: apps in or out of the tunnel ----------

let appsSelection = new Set();

function renderAppsCount() {
  const el = $("#apps-count");
  if (!el) return;
  el.textContent = androidApps.length ? t("apps.selected", androidApps.length) : t("apps.none");
}

function renderAppsList() {
  const list = $("#apps-list");
  const q = $("#apps-search").value.trim().toLowerCase();
  if (!installedApps || !installedApps.length) {
    list.innerHTML = `<div class="empty">${esc(t("apps.empty"))}</div>`;
    return;
  }
  const items = installedApps.filter((a) =>
    !q || (a.label || "").toLowerCase().includes(q) || (a.package || "").toLowerCase().includes(q));
  if (!items.length) {
    list.innerHTML = `<div class="empty">${esc(t("servers.not_found"))}</div>`;
    return;
  }
  list.innerHTML = items.map((a) => `
    <label><input type="checkbox" value="${esc(a.package)}"${appsSelection.has(a.package) ? " checked" : ""}>
      <span class="app-name">${esc(a.label || a.package)}</span><span class="app-pkg">${esc(a.package)}</span></label>`).join("");
}

async function openAppsDialog() {
  appsSelection = new Set(androidApps);
  $("#apps-search").value = "";
  if (!installedApps) {
    try {
      const raw = await api().GetInstalledApps();
      installedApps = JSON.parse(raw || "[]");
    } catch {
      installedApps = [];
      toast(t("apps.failed"), true);
    }
  }
  renderAppsList();
  $("#apps-dialog").showModal();
}

const added = (n) => toast(t("toast.added", n, unitPlural("unit.server_added", n)));

function render() {
  if (!state) return;
  renderDock();
  renderServers();
  renderSubs();
}

// ---------- language ----------

// applyLanguage re-renders everything after a switch of the UI language.
function applyLanguage() {
  api().SetLang(lang).catch(() => {});
  applyLang();
  applyPlatform();
  render();
  renderUpdate();
  if ($("#view-settings").classList.contains("active")) fillSettings();
  enhancedSelects.forEach(syncSelect);
  document.fonts.ready.then(moveTabMark);
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
    // Keep the id: the code hides and shows some rows by it.
    if (lab.id) div.id = lab.id;
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
  enhancedSelects.push(sel);
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
    btn.textContent = t("dock.testing");
    try {
      const ms = await call("TestConnection");
      toast(t("toast.test_ok", ms));
    } catch { /* toast shown */ } finally {
      btn.textContent = t("dock.test");
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
    if (!state.subscriptions.length) return toast(t("toast.no_subs"), true);
    call("UpdateAllSubscriptions").then(() => toast(t("toast.subs_updated"))).catch(() => {});
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
      return call("CopyLink", id).then(() => toast(isConf ? t("toast.copied_conf") : t("toast.copied_link"))).catch(() => {});
    }
    if (act === "del") return call("DeleteServer", id).then(() => toast(t("toast.server_deleted"))).catch(() => {});
    if (act === "login") return askCredentials(id, false);
    if (act === "via") return openViaDialog(id);
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
      toast(t("toast.sub_added", n, unitPlural("unit.server", n)));
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
    if (act === "del") return call("DeleteSubscription", id).then(() => toast(t("toast.sub_deleted"))).catch(() => {});
    if (act === "rename") {
      const sub = state.subscriptions.find((s) => s.id === id);
      $("#rename-input").value = sub.name || "";
      $("#rename-input").placeholder = sub.title || t("d.rename_ph");
      $("#rename-dialog").dataset.id = id;
      return $("#rename-dialog").showModal();
    }
    btn.disabled = true;
    try {
      const n = await call("UpdateSubscription", id);
      toast(t("toast.sub_updated", n, unitPlural("unit.server", n)));
    } catch { /* toast shown */ } finally {
      btn.disabled = false;
    }
  });

  $("#auth-dialog").addEventListener("close", () => {
    const d = $("#auth-dialog");
    if (d.returnValue !== "ok") return;
    const p = call("SetOpenVPNCredentials", d.dataset.id, $("#auth-user").value, $("#auth-pass").value);
    if (d.dataset.connect === "1") p.then(() => call("Connect")).catch(() => {});
    else p.then(() => toast(t("toast.login_saved"))).catch(() => {});
  });
  $("#rename-dialog").addEventListener("close", () => {
    const d = $("#rename-dialog");
    if (d.returnValue === "ok") call("RenameSubscription", d.dataset.id, $("#rename-input").value).catch(() => {});
  });
  $("#via-dialog").addEventListener("close", () => {
    const d = $("#via-dialog");
    if (d.returnValue !== "ok") return;
    const via = $("#via-select").value;
    const relay = state.servers.find((s) => s.id === via);
    call("SetServerVia", d.dataset.id, via)
      .then(() => toast(via ? t("toast.via_set", relay?.name || via) : t("toast.via_off")))
      .catch(() => {});
  });

  // Android: the app list for the split tunnel.
  $("#pick-apps").addEventListener("click", () => openAppsDialog().catch(() => {}));
  $("#apps-search").addEventListener("input", renderAppsList);
  $("#apps-list").addEventListener("change", (e) => {
    const cb = e.target;
    if (cb.type !== "checkbox") return;
    if (cb.checked) appsSelection.add(cb.value);
    else appsSelection.delete(cb.value);
  });
  $("#apps-reset").addEventListener("click", () => {
    appsSelection.clear();
    renderAppsList();
  });
  $("#apps-dialog").addEventListener("close", () => {
    if ($("#apps-dialog").returnValue !== "ok") return;
    androidApps = [...appsSelection];
    renderAppsCount();
    call("SaveSettings", readSettings()).then(() => toast(t("toast.saved"))).catch(() => {});
  });

  // Settings save as soon as a field changes.
  $("#settings-form").addEventListener("submit", (e) => e.preventDefault());
  $("#settings-form").addEventListener("change", (e) => {
    if (e.target.id === "autostart" || e.target.id === "lang-select") return;
    if (e.target.name === "animations") applyMotion(!e.target.checked);
    if (e.target.name === "theme" || e.target.name === "accent") applyLook(e.target.form.theme.value, e.target.form.accent.value);
    if (e.target.name === "balance") $("#balance-strategy").hidden = !e.target.checked;
    // A bad rule keeps the text so it can be corrected.
    const keep = e.target.tagName === "TEXTAREA";
    call("SaveSettings", readSettings()).then(() => toast(t("toast.saved"))).catch(() => keep || fillSettings());
  });
  $("#lang-select").addEventListener("change", (e) => {
    setLang(e.target.value, false);
    applyLanguage();
  });
  $("#autostart").addEventListener("change", (e) =>
    call("SetAutostart", e.target.checked).then(renderAutostart).catch(() => api().GetAutostart().then(renderAutostart)));
  $("#open-dir").addEventListener("click", () => api().OpenDataDir());
  $("#logs-refresh").addEventListener("click", refreshLogs);

  $("#check-update").addEventListener("click", () => call("CheckUpdate").catch(() => {}));
  $("#update-go").addEventListener("click", applyUpdate);
  $("#update-notes").addEventListener("click", () => {
    $("#notes-title").textContent = t("d.notes_version", bareVersion(upd.latest));
    $("#notes-body").textContent = upd.notes || t("d.notes_none");
    $("#notes-dialog").showModal();
  });
  $("#notes-dialog").addEventListener("close", () => {
    if ($("#notes-dialog").returnValue === "update") applyUpdate();
  });

  window.runtime.EventsOn("state", (s) => {
    const prevError = state?.status.error;
    state = s;
    applyLook(s.settings.theme, s.settings.accent);
    render();
    if (s.status.error === "need_admin" && prevError !== "need_admin" && !$("#admin-dialog").open) $("#admin-dialog").showModal();
    if (s.status.error === "need_ovpn_auth" && prevError !== "need_ovpn_auth" && !$("#auth-dialog").open) askCredentials(s.status.serverId, true);
  });
  window.runtime.EventsOn("traffic", renderTraffic);
  window.runtime.EventsOn("notice", (text) => toast(trGo(text)));
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
  tunOption.textContent = t("s.mode_tun_unix");
}

// Android always routes all traffic through the VPN and starts with the
// system (always-on VPN), so the mode and autostart controls give way.
function applyAndroid() {
  $("#mode-seg").hidden = true;
  $("#qr-btn").title = t("servers.qr_android");
  $('#settings-form select[name="mode"]').closest(".row-field").hidden = true;
  $("#autostart").closest("label").hidden = true;
  $("#autostart-hint").textContent = t("s.autostart_h_android");
  if (!document.querySelector("#vpn-settings-btn")) {
    const vpnSettings = document.createElement("div");
    vpnSettings.className = "inline";
    vpnSettings.innerHTML = `<button class="btn ghost" type="button" id="vpn-settings-btn">${esc(t("s.vpn_settings_btn"))}</button>`;
    vpnSettings.firstChild.addEventListener("click", () => api().OpenVpnSettings());
    $("#autostart-hint").after(vpnSettings);
  } else {
    $("#vpn-settings-btn").textContent = t("s.vpn_settings_btn");
  }
  $('#settings-form input[name="autoConnect"]').parentElement.querySelector("span").textContent = t("s.auto_connect_android");
  $("#open-dir").closest(".inline").hidden = true;
  $("#dns-hint").textContent = t("s.dns_hint_android");
  // The per-app split of the tunnel replaces the process rules on Android.
  $("#android-apps").hidden = false;
  $("#apps-section").hidden = true;
  renderAppsCount();
}

async function init() {
  bind();
  setLang(lang, false);
  langChanged = applyLanguage;
  // The tray menu starts in Russian; bring it to the saved language.
  api().SetLang(lang).catch(() => {});
  let trafficNow;
  [state, upd, platform, trafficNow] = await Promise.all([api().GetState(), api().GetUpdate(), api().GetPlatform(), api().GetTraffic()]);
  applyMotion(state.settings.noAnimations);
  applyLook(state.settings.theme, state.settings.accent);
  applyPlatform();
  moveTabMark();
  renderTraffic(trafficNow);
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
