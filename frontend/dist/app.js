"use strict";

const api = () => window.go.main.App;
const $ = (sel) => document.querySelector(sel);

let state = null;
const pings = {};
let logsTimer = null;

// ---------- helpers ----------

function toast(msg, isError = false) {
  const el = document.createElement("div");
  el.className = "toast" + (isError ? " error" : "");
  el.textContent = msg;
  $("#toasts").appendChild(el);
  setTimeout(() => el.remove(), isError ? 7000 : 3500);
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
  return unix ? new Date(unix * 1000).toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" }) : "—";
}

// ---------- rendering ----------

function render() {
  if (!state) return;
  renderConn();
  renderServers();
  renderSubs();
  $("#admin-badge").textContent = state.status.admin ? "Запущено от администратора" : "";
}

function renderConn() {
  const st = state.status;
  const conn = $("#conn");
  conn.classList.toggle("connected", st.state === "connected");
  conn.classList.toggle("connecting", st.state === "connecting" || st.state === "disconnecting");

  $("#conn-state").textContent = { connected: "Подключено", connecting: "Подключение…", disconnecting: "Отключение…", disconnected: "Отключено" }[st.state];
  const srv = state.servers.find((s) => s.id === (st.state === "disconnected" ? state.selected : st.serverId));
  let line = srv ? `${srv.name} · ${srv.address}:${srv.port}` : "Сервер не выбран";
  if (st.state === "connected") {
    line += st.mode === "tun" ? ` · TUN через «${st.interface}»` : ` · прокси 127.0.0.1:${state.settings.httpPort}`;
  }
  const sum = srv?.subId ? subSummary(state.subscriptions.find((x) => x.id === srv.subId)) : "";
  $("#conn-server").textContent = line;
  $("#conn-sub").textContent = sum ? `Подписка: ${sum}` : "";
  $("#conn-error").textContent =
    st.error === "need_admin" ? "Для TUN нужны права администратора" :
    st.error === "need_ovpn_auth" ? "OpenVPN: нужны логин и пароль" : st.error || "";
  $("#test-btn").disabled = st.state !== "connected";

  // WireGuard/AmneziaWG are full tunnels: they always run in TUN mode.
  const selected = state.servers.find((x) => x.id === (st.state === "disconnected" ? state.selected : st.serverId));
  const fullTunnelOnly = !!(selected?.wg || selected?.ovpn);
  const mode = fullTunnelOnly ? "tun" : state.settings.mode;
  document.querySelectorAll("#mode-seg button").forEach((b) => {
    b.classList.toggle("active", b.dataset.mode === mode);
    const locked = fullTunnelOnly && b.dataset.mode === "proxy";
    b.disabled = locked;
    b.title = locked ? "WireGuard, AmneziaWG и OpenVPN работают только в режиме полного туннеля (TUN)" : "";
  });
  $("#admin-proxy").hidden = fullTunnelOnly;
}

function pingCell(id) {
  const ms = pings[id];
  if (ms === undefined) return `<div class="ping"></div>`;
  if (ms === null) return `<div class="ping">…</div>`;
  if (ms === -2) return `<div class="ping" title="WireGuard не отвечает на проверки — проверьте подключением">н/д</div>`;
  if (ms < 0) return `<div class="ping bad">таймаут</div>`;
  const cls = ms < 150 ? "good" : ms < 400 ? "mid" : "bad";
  return `<div class="ping ${cls}">${ms} мс</div>`;
}

const PROTO_LABEL = { shadowsocks: "ss", hysteria2: "hy2", amneziawg: "awg", wireguard: "wg", openvpn: "ovpn" };

function serverTags(s) {
  const tags = [PROTO_LABEL[s.protocol] || s.protocol];
  if (s.wg) return tags;
  if (s.ovpn) return [...tags, s.network];
  if (s.accessKeyUrl) tags.push("outline");
  tags.push(s.network);
  if (s.security !== "none") tags.push(s.security);
  if (s.obfs) tags.push(s.obfs);
  if (s.hopPorts) tags.push("hop");
  return tags;
}

function renderServers() {
  const q = $("#search").value.trim().toLowerCase();
  const servers = state.servers.filter((s) => !q || s.name.toLowerCase().includes(q) || s.address.toLowerCase().includes(q));
  const list = $("#server-list");
  if (!state.servers.length) {
    list.innerHTML = `<div class="empty">Серверов пока нет.<br>Скопируйте ссылку vless:// или URL подписки и нажмите «Вставить из буфера».</div>`;
    return;
  }

  const subGroup = (id) => {
    const sub = state.subscriptions.find((s) => s.id === id);
    const sum = subSummary(sub);
    return subTitle(sub) + (sum ? " · " + sum : "");
  };
  let html = "";
  let lastGroup = null;
  for (const s of servers) {
    const group = s.subId ? subGroup(s.subId) : "Добавленные вручную";
    if (group !== lastGroup) {
      html += `<div class="group">${esc(group)}</div>`;
      lastGroup = group;
    }
    const tags = serverTags(s);
    html += `
      <div class="item ${s.id === state.selected ? "selected" : ""}" data-id="${s.id}">
        <div class="radio"></div>
        <div class="item-main">
          <div class="item-name">${esc(s.name)}</div>
          <div class="item-sub">${tags.map((t) => `<span class="tag">${esc(t)}</span>`).join("")}${esc(s.address)}:${s.port}</div>
        </div>
        ${pingCell(s.id)}
        <div class="item-actions">
          ${s.ovpn?.needAuth ? `<button class="btn small" data-act="login" title="Логин и пароль OpenVPN">Логин</button>` : ""}
          <button class="btn small" data-act="copy" title="Скопировать ссылку">Ссылка</button>
          <button class="btn small danger" data-act="del" title="Удалить">✕</button>
        </div>
      </div>`;
  }
  list.innerHTML = html || `<div class="empty">Ничего не найдено</div>`;
}

function subTitle(sub) {
  return sub?.name || sub?.title || "Подписка";
}

function plural(n, one, few, many) {
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

function daysLeft(sub) {
  return Math.ceil((sub.expire * 1000 - Date.now()) / 86400000);
}

// Short "осталось 45.3 ГБ · 29 дней" summary for list headers and the connection card.
function subSummary(sub) {
  if (!sub) return "";
  const parts = [];
  if (sub.hasUserInfo) {
    parts.push(sub.total ? `осталось ${fmtBytes(Math.max(0, sub.total - sub.upload - sub.download))}` : "безлимит");
  }
  if (sub.expire) {
    const days = daysLeft(sub);
    parts.push(days > 0 ? `${days} ${plural(days, "день", "дня", "дней")}` : "истекла");
  }
  return parts.join(" · ");
}

function trafficBlock(sub) {
  if (!sub.hasUserInfo) return `<div class="sub-line muted">Провайдер не сообщает расход трафика</div>`;
  const used = sub.upload + sub.download;
  const detail = `↑ ${fmtBytes(sub.upload)} · ↓ ${fmtBytes(sub.download)}`;
  if (!sub.total) {
    return `<div class="sub-line"><b>Безлимитный трафик</b> <span class="muted">· использовано ${fmtBytes(used)} (${detail})</span></div>`;
  }
  const left = Math.max(0, sub.total - used);
  const pct = Math.min(100, (used / sub.total) * 100);
  const cls = pct >= 90 ? "bad" : pct >= 70 ? "mid" : "";
  return `
    <div class="sub-line"><b>Осталось ${fmtBytes(left)}</b> <span class="muted">из ${fmtBytes(sub.total)} · использовано ${fmtBytes(used)} (${detail})</span></div>
    <div class="bar ${cls}"><div style="width:${pct.toFixed(1)}%"></div></div>`;
}

function expireBlock(sub) {
  if (!sub.hasUserInfo && !sub.expire) return "";
  if (!sub.expire) return `<div class="sub-line">Бессрочно</div>`;
  const date = new Date(sub.expire * 1000).toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric" });
  const days = daysLeft(sub);
  if (days <= 0) return `<div class="sub-line bad-text">Истекла ${date}</div>`;
  const cls = days <= 3 ? "bad-text" : days <= 7 ? "mid-text" : "";
  return `<div class="sub-line ${cls}">Действует до ${date} · ещё ${days} ${plural(days, "день", "дня", "дней")}</div>`;
}

function renderSubs() {
  const list = $("#sub-list");
  if (!state.subscriptions.length) {
    list.innerHTML = `<div class="empty">Подписок нет. Вставьте URL подписки от вашего провайдера.</div>`;
    return;
  }
  list.innerHTML = state.subscriptions.map((s) => {
    const count = state.servers.filter((x) => x.subId === s.id).length;
    const every = s.updateInterval || 12;
    return `
      <div class="sub-card" data-id="${s.id}">
        <div class="sub-head">
          <div class="item-main">
            <div class="sub-title">${esc(subTitle(s))}</div>
            <div class="item-sub">${esc(s.url)}</div>
          </div>
          <div class="sub-buttons">
            ${s.webPageUrl ? `<button class="btn small" data-act="open" data-url="${esc(s.webPageUrl)}">Сайт</button>` : ""}
            ${s.supportUrl ? `<button class="btn small" data-act="open" data-url="${esc(s.supportUrl)}">Поддержка</button>` : ""}
            <button class="btn small" data-act="update">Обновить</button>
            <button class="btn small" data-act="rename">Переименовать</button>
            <button class="btn small danger" data-act="del">Удалить</button>
          </div>
        </div>
        ${s.announce ? `<div class="announce">${esc(s.announce)}</div>` : ""}
        ${trafficBlock(s)}
        ${expireBlock(s)}
        <div class="sub-line muted">${count} ${plural(count, "сервер", "сервера", "серверов")} · обновлено ${fmtDate(s.updated)} · автообновление каждые ${every} ч</div>
        ${s.lastError ? `<div class="sub-line bad-text">Ошибка последнего обновления: ${esc(s.lastError)}</div>` : ""}
      </div>`;
  }).join("");
}

function fillSettings() {
  const f = $("#settings-form");
  const s = state.settings;
  f.mode.value = s.mode;
  f.socksPort.value = s.socksPort;
  f.httpPort.value = s.httpPort;
  f.dns.value = (s.dns || []).join(", ");
  f.bypassRu.checked = s.bypassRu;
  f.sound.checked = !s.soundOff;
  f.autoConnect.checked = s.autoConnect;
  api().GetAutostart().then(renderAutostart);
  f.logLevel.value = s.logLevel;
}

function renderAutostart(st) {
  $("#autostart").checked = st.enabled;
  const hint = $("#autostart-hint");
  hint.classList.remove("warn");
  if (!st.enabled) {
    hint.textContent = st.admin
      ? "Клиент будет запускаться с правами администратора, без запроса UAC — TUN тоже заработает."
      : "Сейчас клиент запущен без прав администратора: автозапуск будет работать только в режиме системного прокси. Для TUN включите автозапуск, запустив Vlessia от администратора.";
  } else if (st.elevated) {
    hint.textContent = "Запускается с правами администратора (через Планировщик заданий): работают все режимы.";
  } else {
    hint.textContent = "Запускается без прав администратора: TUN, WireGuard, AmneziaWG и OpenVPN не подключатся автоматически. Чтобы исправить, запустите Vlessia от администратора и включите автозапуск заново.";
    hint.classList.add("warn");
  }
}

// ---------- actions ----------

async function refreshLogs() {
  const pre = $("#logs");
  const atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 20;
  pre.textContent = await api().GetLogs();
  if (atBottom) pre.scrollTop = pre.scrollHeight;
}

function showView(name) {
  document.querySelectorAll(".nav").forEach((b) => b.classList.toggle("active", b.dataset.view === name));
  document.querySelectorAll(".view").forEach((v) => v.classList.toggle("active", v.id === "view-" + name));
  clearInterval(logsTimer);
  if (name === "settings") fillSettings();
  if (name === "logs") {
    refreshLogs();
    logsTimer = setInterval(() => $("#logs-auto").checked && refreshLogs(), 2000);
  }
}

// askCredentials asks for an OpenVPN login; connect=true reconnects after.
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

async function importText(text) {
  const n = await call("ImportText", text);
  toast(`Добавлено серверов: ${n}`);
}

function bind() {
  document.querySelectorAll(".nav").forEach((b) => b.addEventListener("click", () => showView(b.dataset.view)));

  $("#power").addEventListener("click", () => call("Toggle").catch(() => {}));

  document.querySelectorAll("#mode-seg button").forEach((b) =>
    b.addEventListener("click", () => call("SetMode", b.dataset.mode).catch(() => {})));

  $("#test-btn").addEventListener("click", async () => {
    const btn = $("#test-btn");
    btn.textContent = "Проверка…";
    try {
      const ms = await call("TestConnection");
      toast(`Интернет через VPN работает, задержка ${ms} мс`);
    } catch { /* toast shown */ } finally {
      btn.textContent = "Проверить";
    }
  });

  $("#paste-btn").addEventListener("click", async () => {
    const n = await call("ImportClipboard").catch(() => null);
    if (n !== null) toast(`Добавлено серверов: ${n}`);
  });

  $("#file-btn").addEventListener("click", async () => {
    const n = await call("ImportFile").catch(() => null);
    if (n) toast(`Добавлено серверов: ${n}`);
  });

  $("#add-btn").addEventListener("click", () => {
    $("#add-text").value = "";
    $("#add-dialog").showModal();
  });
  $("#add-dialog").addEventListener("close", () => {
    if ($("#add-dialog").returnValue === "ok") importText($("#add-text").value).catch(() => {});
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

  $("#update-subs-btn").addEventListener("click", async () => {
    if (!state.subscriptions.length) return toast("Подписок нет", true);
    await call("UpdateAllSubscriptions").then(() => toast("Подписки обновлены")).catch(() => {});
  });

  $("#search").addEventListener("input", renderServers);

  $("#server-list").addEventListener("click", (e) => {
    const item = e.target.closest(".item");
    if (!item) return;
    const id = item.dataset.id;
    const act = e.target.closest("[data-act]")?.dataset.act;
    if (act === "copy") {
      const isConf = !state.servers.find((x) => x.id === id)?.link.includes("://");
      return call("CopyLink", id).then(() => toast(isConf ? "Конфигурация скопирована" : "Ссылка скопирована")).catch(() => {});
    }
    if (act === "del") return call("DeleteServer", id).catch(() => {});
    if (act === "login") return askCredentials(id, false);
    if (id !== state.selected) call("SelectServer", id).catch(() => {});
  });

  $("#server-list").addEventListener("dblclick", (e) => {
    const item = e.target.closest(".item");
    if (item && !e.target.closest("[data-act]") && state.status.state === "disconnected") {
      call("SelectServer", item.dataset.id).then(() => call("Connect")).catch(() => {});
    }
  });

  $("#sub-add").addEventListener("click", async () => {
    const url = $("#sub-url").value.trim();
    if (!url) return;
    const btn = $("#sub-add");
    btn.disabled = true;
    try {
      const n = await call("AddSubscription", $("#sub-name").value.trim(), url);
      toast(`Подписка добавлена, серверов: ${n}`);
      $("#sub-url").value = "";
      $("#sub-name").value = "";
    } catch { /* toast shown */ } finally {
      btn.disabled = false;
    }
  });

  $("#sub-list").addEventListener("click", async (e) => {
    const btn = e.target.closest("[data-act]");
    const id = e.target.closest(".sub-card")?.dataset.id;
    if (!btn || !id) return;
    const act = btn.dataset.act;
    if (act === "open") return api().OpenURL(btn.dataset.url);
    if (act === "del") return call("DeleteSubscription", id).catch(() => {});
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
      toast(`Обновлено, серверов: ${n}`);
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

  $("#settings-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const f = e.target;
    const s = {
      mode: f.mode.value,
      socksPort: Number(f.socksPort.value),
      httpPort: Number(f.httpPort.value),
      dns: f.dns.value.split(/[,\s]+/).filter(Boolean),
      bypassRu: f.bypassRu.checked,
      soundOff: !f.sound.checked,
      autoConnect: f.autoConnect.checked,
      logLevel: f.logLevel.value,
    };
    await call("SaveSettings", s).then(() => toast("Настройки сохранены")).catch(() => {});
  });

  $("#open-dir").addEventListener("click", () => api().OpenDataDir());
  $("#autostart").addEventListener("change", (e) =>
    call("SetAutostart", e.target.checked).then(renderAutostart).catch(() => api().GetAutostart().then(renderAutostart)));
  $("#logs-refresh").addEventListener("click", refreshLogs);

  window.runtime.EventsOn("state", (s) => {
    const prevError = state?.status.error;
    state = s;
    render();
    if (s.status.error === "need_admin" && prevError !== "need_admin" && !$("#admin-dialog").open) $("#admin-dialog").showModal();
    if (s.status.error === "need_ovpn_auth" && prevError !== "need_ovpn_auth" && !$("#auth-dialog").open) askCredentials(s.status.serverId, true);
  });
  window.runtime.EventsOn("traffic", (t) => {
    $("#up").textContent = fmtSpeed(t.up);
    $("#down").textContent = fmtSpeed(t.down);
  });
  window.runtime.EventsOn("ping", (p) => {
    pings[p.id] = p.ms;
    const item = document.querySelector(`#server-list .item[data-id="${p.id}"] .ping`);
    if (item) item.outerHTML = pingCell(p.id);
  });
}

async function init() {
  bind();
  state = await api().GetState();
  render();
  $("#version").textContent = "Vlessia " + (await api().GetVersion());
}

// Wails injects its runtime asynchronously; wait until bindings are ready.
(function waitForRuntime() {
  if (window.go?.main?.App && window.runtime) init();
  else setTimeout(waitForRuntime, 20);
})();
