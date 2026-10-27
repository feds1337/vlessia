// Android bridge, loaded before app.js. It provides what Wails provides on
// the desktop: window.go.main.App.* as promises resolved by the native side
// (MainActivity), and window.runtime.EventsOn for events from the Go core.
"use strict";

(() => {
  const pending = new Map();
  let seq = 0;

  window.__vlessiaResolve = (id, ok, payload) => {
    const p = pending.get(id);
    if (!p) return;
    pending.delete(id);
    if (ok) p.resolve(payload === "" ? null : JSON.parse(payload));
    else p.reject(payload);
  };

  const App = new Proxy({}, {
    get: (_, method) => (...args) => new Promise((resolve, reject) => {
      const id = ++seq;
      pending.set(id, { resolve, reject });
      VlessiaNative.call(id, String(method), JSON.stringify(args));
    }),
  });
  window.go = { main: { App } };

  const listeners = {};
  window.runtime = {
    EventsOn(name, fn) { (listeners[name] ||= []).push(fn); },
  };
  window.__vlessiaEmit = (name, data) => (listeners[name] || []).forEach((fn) => fn(data));

  // Back button: close a dialog, then return to the server list; false
  // lets Android send the app to the background.
  window.__vlessiaBack = () => {
    const open = document.querySelector("dialog[open]");
    if (open) {
      open.close("cancel");
      return true;
    }
    const servers = document.querySelector('.tab[data-view="servers"]');
    if (servers && !servers.classList.contains("active")) {
      servers.click();
      return true;
    }
    return false;
  };

  document.documentElement.classList.add("android");
})();
