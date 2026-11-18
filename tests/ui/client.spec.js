const { test, expect } = require('@playwright/test');

// Exercise the actual HTML/JS with a deterministic native bridge. Network,
// filesystem and protocol behavior are covered by Go tests.
async function boot(page, options = {}) {
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(({ platform, recovered }) => {
    const listeners = {};
    window.windowCalls = [];
    window.windowMaximised = false;
    window.runtime = {
      EventsOn: (name, fn) => (listeners[name] ||= []).push(fn),
      WindowMinimise: () => window.windowCalls.push('minimise'),
      WindowToggleMaximise: () => { window.windowMaximised = !window.windowMaximised; window.windowCalls.push('maximise'); },
      WindowIsMaximised: async () => window.windowMaximised,
      Quit: () => window.windowCalls.push('close'),
    };
    const emit = () => (listeners.state || []).forEach(fn => fn(structuredClone(data)));
    const data = {
      servers: [], subscriptions: [], selected: '',
      status: { state: 'disconnected', mode: 'proxy', admin: true },
      settings: { mode: 'proxy', socksPort: 10808, httpPort: 10809, dns: ['1.1.1.1'], logLevel: 'warning', noAnimations: true, proxyRules: [], directRules: [], proxyApps: [], directApps: [] },
    };
    window.testData = data;
    window.testCalls = [];
    const methods = {
      GetState: () => structuredClone(data), GetPlatform: () => platform,
      GetVersion: () => 'v0.8.1', GetUpdate: () => ({ state: 'idle', current: 'v0.8.1' }),
      GetTraffic: () => ({ history: [], sessionUp: 0, sessionDown: 0, todayUp: 0, todayDown: 0 }), GetAutostart: () => ({ admin: true }),
      WelcomePending: () => false, ConfigRecovered: () => recovered,
      GetTrafficHistory: () => {
        if (window.historyFails) throw new Error('History unavailable');
        return window.testHistory || { days: [], connections: [] };
      },
      SetLang: () => {},
      ImportText: text => {
        data.servers = [{ id: 's', name: 'Test server', address: '127.0.0.1', port: 443, protocol: 'vless', network: 'tcp' }];
        data.selected = 's'; emit(); return 1;
      },
      SaveSettings: settings => {
        if (window.rejectSave) throw new Error('Disk full');
        data.settings = settings; emit();
      },
      ValidateRule: (kind, value) => {
        if (value.includes('[') || value.includes('\n')) throw new Error('Invalid rule');
        return value.trim();
      },
      Toggle: () => { data.status.state = data.status.state === 'connected' ? 'disconnected' : 'connected'; data.status.serverId = 's'; emit(); },
      Diagnose: () => [{ check: 'https', result: 'failed', ms: 0 }],
      AddSubscription: (name, url) => { data.subscriptions.push({ id: 'sub', name, url }); emit(); return 1; },
    };
    window.go = { main: { App: new Proxy({}, { get: (_, name) => async (...args) => {
      window.testCalls.push([name, args]);
      if (!(name in methods)) throw new Error('Unexpected bridge method: ' + name);
      return methods[name](...args);
    } }) } };
  }, { platform: options.platform || 'windows', recovered: !!options.recovered });
  await page.goto('/');
  await expect(page.locator('#power')).toHaveText('Подключить');
  return errors;
}

async function settings(page) {
  await page.locator('.tab[data-view="settings"]').click();
  await expect(page.locator('#view-settings')).toHaveClass(/active/);
}

test('import, connect and diagnose a server', async ({ page }) => {
  const errors = await boot(page);
  await page.locator('#add-btn').click();
  await page.locator('#add-text').fill('vless://11111111-1111-1111-1111-111111111111@127.0.0.1:443#Test');
  await page.locator('#add-dialog button[value="ok"]').click();
  await expect(page.locator('#server-list')).toContainText('Test server');
  await page.locator('#power').click();
  await expect(page.locator('#power')).toHaveText('Отключить');
  await page.locator('#test-btn').click();
  await expect(page.locator('#diagnostic-results')).toContainText('Обновите подписку');
  expect(errors).toEqual([]);
});

test('subscription form and language switch', async ({ page }) => {
  const errors = await boot(page);
  await page.locator('.tab[data-view="subs"]').click();
  await page.locator('#sub-url').fill('https://example.com/sub');
  await page.locator('#sub-name').fill('My subscription');
  await page.locator('#sub-add').click();
  await expect(page.locator('#sub-list')).toContainText('My subscription');
  await settings(page);
  await page.locator('#lang-select').selectOption('en', { force: true });
  await expect(page.locator('#rule-builder-open')).toHaveText('Add rule…');
  expect(errors).toEqual([]);
});

test('rule builder validates errors, preserves existing settings and handles failed saves', async ({ page }) => {
  const errors = await boot(page);
  await settings(page);
  await page.locator('#settings-tab-rules').click();
  await page.locator('#rule-builder-open').click();
  await page.locator('#rule-value').fill('regexp:[');
  await page.locator('#rule-save').click();
  await expect(page.locator('#rule-error')).toHaveText('Invalid rule');
  await page.locator('#rule-value').fill('youtube.com');
  await page.evaluate(() => window.rejectSave = true);
  await page.locator('#rule-save').click();
  await expect(page.locator('#rule-error')).toHaveText('Disk full');
  await expect(page.locator('#rule-builder')).toBeVisible();
  await page.evaluate(() => window.rejectSave = false);
  await page.locator('#rule-save').click();
  await expect(page.locator('#rule-builder')).not.toBeVisible();
  await expect(page.locator('textarea[name="proxyRules"]')).toHaveValue('youtube.com');
  expect(await page.evaluate(() => testData.settings.dns)).toEqual(['1.1.1.1']);
  await page.locator('#rule-builder-open').click();
  await page.locator('#rule-route').selectOption('direct');
  await page.locator('#rule-value').fill('youtube.com');
  await page.locator('#rule-save').click();
  await expect(page.locator('#rule-error')).toContainText('удалите');
  expect(await page.evaluate(() => testData.settings.directRules)).toEqual(['']);
  await page.locator('#rule-kind').selectOption('app');
  await page.locator('#rule-route').selectOption('direct');
  await page.locator('#rule-value').fill('steam.exe');
  await page.locator('#rule-save').click();
  await expect(page.locator('textarea[name="directApps"]')).toHaveValue('steam.exe');
  expect(errors).toEqual([]);
});

test('Android recovery notice and site rules', async ({ page }) => {
  const errors = await boot(page, { platform: 'android', recovered: true });
  await expect(page.locator('#toasts')).toContainText('восстановлены');
  await settings(page);
  await page.locator('#settings-tab-rules').click();
  await page.locator('#rule-builder-open').click();
  await expect(page.locator('#rule-kind option[value="app"]')).toHaveAttribute('disabled', '');
  await page.locator('#rule-value').fill('example.org');
  await page.locator('#rule-save').click();
  await expect(page.locator('textarea[name="proxyRules"]')).toHaveValue('example.org');
  expect(errors).toEqual([]);
});


test('custom title bar follows theme and dispatches native window commands', async ({ page }) => {
  const errors = await boot(page);
  await expect(page.locator('#titlebar')).toBeVisible();
  await expect(page.locator('.titlebar-brand')).toHaveText('Vlessia');
  await expect(page.locator('.dock .brand')).not.toBeVisible();
  await page.locator('#window-maximise').click();
  await expect(page.locator('#window-maximise')).toHaveAccessibleName('Восстановить размер');
  await page.locator('#titlebar-drag').dblclick();
  await expect(page.locator('#window-maximise')).toHaveAccessibleName('Развернуть');
  await page.locator('#window-minimise').click();
  await page.locator('#window-close').click();
  expect(await page.evaluate(() => window.windowCalls)).toEqual(['maximise', 'maximise', 'minimise', 'close']);
  await page.evaluate(() => applyLook('dark', 'violet'));
  const dark = await page.locator('#titlebar').evaluate(el => getComputedStyle(el).backgroundColor);
  await page.evaluate(() => applyLook('light', 'violet'));
  const light = await page.locator('#titlebar').evaluate(el => getComputedStyle(el).backgroundColor);
  expect(dark).not.toEqual(light);
  expect(await page.locator('#titlebar-drag').evaluate(el => getComputedStyle(el).getPropertyValue('--wails-draggable').trim())).toBe('drag');
  expect(await page.locator('#window-close').evaluate(el => getComputedStyle(el).getPropertyValue('--wails-draggable').trim())).toBe('no-drag');
  expect(errors).toEqual([]);
});

test('desktop title bar stays off Android', async ({ page }) => {
  await boot(page, { platform: 'android' });
  await expect(page.locator('#titlebar')).not.toBeVisible();
  await expect(page.locator('.dock .brand')).toBeVisible();
});

test('title bar layout stays above the content in both themes', async ({ page }, testInfo) => {
  await page.setViewportSize({width: 1060, height: 700});
  await boot(page);
  await page.evaluate(async () => {
    testData.servers = [
      {id:'s',name:'Нидерланды · Амстердам',protocol:'vless',network:'tcp',security:'reality',address:'nl.example.com',port:443},
      {id:'de',name:'Германия · Франкфурт',protocol:'vless',network:'tcp',security:'reality',address:'de.example.com',port:443},
      {id:'fi',name:'Финляндия · Хельсинки',protocol:'trojan',network:'tcp',security:'tls',address:'fi.example.com',port:443}
    ];
    testData.selected = 's';
    await window.go.main.App.Toggle();
    await window.go.main.App.Toggle();
    applyLook('dark','');
    await document.fonts.ready;
  });
  const header = await page.locator('#titlebar').boundingBox();
  const dock = await page.locator('#dock').boundingBox();
  expect(header.y + header.height).toBe(dock.y);
  await page.screenshot({path:testInfo.outputPath('titlebar-dark.png')});
  await page.evaluate(() => applyLook('light',''));
  await page.screenshot({path:testInfo.outputPath('titlebar-light.png')});
});

test('speed graph pauses once settled and resumes on the next sample', async ({ page }) => {
  await boot(page);
  const result = await page.evaluate(() => {
    state.status.state = 'connected';
    document.documentElement.classList.remove('no-anim');
    const sample = {history: [{down: 100}, {down: 200}], up: 0, down: 0};
    renderTraffic(sample);
    const animating = drawGraph(live.at + 100, 0.03);
    const settled = drawGraph(live.at + 1500, 0.03);
    renderTraffic(sample);
    const resumed = drawGraph(live.at + 100, 0.03);
    state.status.state = 'disconnected';
    drawGraph(live.at + 200, 0.03);
    return { animating, settled, resumed, cleared: !document.querySelector('#graph-down').hasAttribute('d') };
  });
  expect(result).toEqual({animating: true, settled: false, resumed: true, cleared: true});
});

async function seedTraffic(page) {
  await page.evaluate(() => {
    const today = new Date();
    today.setHours(12, 0, 0, 0);
    const days = Array.from({ length: 30 }, (_, i) => {
      const date = new Date(today);
      date.setDate(date.getDate() - 29 + i);
      const day = [date.getFullYear(), String(date.getMonth() + 1).padStart(2, '0'), String(date.getDate()).padStart(2, '0')].join('-');
      const mib = 2 ** 20;
      return { day, up: ((i * 11) % 35 + 9) * mib, down: ((i * 17) % 170 + 35) * mib };
    });
    window.testHistory = {
      days, connections: [
        { started: today / 1000 - 10 * 86400, server: 'Франкфурт', mode: 'tun' },
        { started: today / 1000 - 86400, server: 'Хельсинки', mode: 'proxy' },
        { started: today / 1000, server: 'Амстердам', mode: 'tun' },
      ],
    };
  });
}

for (const platform of ['windows', 'android']) {
  test('settings and traffic workspaces on ' + platform, async ({ page }, testInfo) => {
    const mobile = platform === 'android';
    await page.setViewportSize(mobile ? { width: 360, height: 740 } : { width: 1060, height: 700 });
    const errors = await boot(page, { platform });
    await seedTraffic(page);
    await settings(page);
    await expect(page.locator('#settings-tab-general')).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('#backup-export')).toBeHidden();
    await expect(page.locator('#power')).toBeVisible();
    if (mobile) await expect(page.locator('#test-btn')).toBeHidden();
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: testInfo.outputPath('settings-' + platform + '.png') });
    for (const id of ['network', 'rules', 'data', 'general']) {
      await page.locator('#settings-tab-' + id).click();
      await expect(page.locator('#settings-' + id)).toBeVisible();
      expect(await page.locator('#settings-form').evaluate(el => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1);
    }
    await page.locator('.tab[data-view="traffic"]').click();
    await expect(page.locator('#traffic-connections li')).toHaveCount(3);
    const monthTotal = await page.locator('#traffic-summary').innerText();
    await page.locator('[data-days="7"]').click();
    await expect(page.locator('[data-days="7"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#traffic-summary')).not.toHaveText(monthTotal);
    await expect(page.locator('#traffic-connections li')).toHaveCount(2);
    await page.locator('#traffic-detail summary').click();
    await expect(page.locator('#traffic-days tr')).toHaveCount(7);
    await page.locator('#traffic-detail summary').click();
    await page.locator('[data-days="30"]').click();
    await expect(page.locator('#traffic-summary')).toHaveText(monthTotal);
    expect(await page.locator('#view-traffic .list').evaluate(el => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1);
    await page.screenshot({ path: testInfo.outputPath('traffic-' + platform + '.png') });
    await page.locator('.tab[data-view="servers"]').click();
    await expect(page.locator('#test-btn')).toBeVisible();
    expect(errors).toEqual([]);
  });
}

test('traffic history recovers from failure and explains an unused period', async ({ page }) => {
  const errors = await boot(page);
  await page.evaluate(() => { window.historyFails = true; });
  await page.locator('.tab[data-view="traffic"]').click();
  await expect(page.locator('#traffic-retry')).toBeVisible();
  await seedTraffic(page);
  await page.evaluate(() => {
    window.historyFails = false;
    window.testHistory.days.forEach(day => { day.up = 0; day.down = 0; });
    window.testHistory.connections = [];
  });
  await page.locator('#traffic-retry').click();
  await expect(page.locator('#traffic-no-data')).toBeVisible();
  await expect(page.locator('#traffic-no-connections')).toBeVisible();
  await expect(page.locator('#traffic-retry')).toBeHidden();
  expect(errors).toEqual([]);
});

test('settings autosave confirms edits and restores a failed visual change', async ({ page }) => {
  await boot(page);
  await settings(page);
  await page.locator('input[name="sound"]').check();
  await expect(page.locator('#settings-save')).toHaveText('Сохранено');
  expect(await page.evaluate(() => testData.settings.sound)).toBe(true);
  await page.evaluate(() => { window.rejectSave = true; });
  await page.locator('input[name="animations"]').click();
  await expect(page.locator('#settings-save')).toHaveText('Ошибка сохранения');
  await expect(page.locator('input[name="animations"]')).not.toBeChecked();
  await expect(page.locator('html')).toHaveClass(/no-anim/);
  await page.locator('#settings-tab-general').focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('#settings-network')).toBeVisible();
});
