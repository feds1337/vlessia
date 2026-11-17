const { test, expect } = require('@playwright/test');

// Exercise the actual HTML/JS with a deterministic native bridge. Network,
// filesystem and protocol behavior are covered by Go tests.
async function boot(page, options = {}) {
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.addInitScript(({ platform, recovered }) => {
    const listeners = {};
    window.runtime = { EventsOn: (name, fn) => (listeners[name] ||= []).push(fn) };
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
      GetVersion: () => 'v0.7.1', GetUpdate: () => ({ state: 'idle', current: 'v0.7.1' }),
      GetTraffic: () => ({ history: [] }), GetAutostart: () => ({}),
      WelcomePending: () => false, ConfigRecovered: () => recovered,
      GetTrafficHistory: () => ({ days: [], connections: [] }),
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
  await page.locator('#rule-builder-open').click();
  await expect(page.locator('#rule-kind option[value="app"]')).toHaveAttribute('disabled', '');
  await page.locator('#rule-value').fill('example.org');
  await page.locator('#rule-save').click();
  await expect(page.locator('textarea[name="proxyRules"]')).toHaveValue('example.org');
  expect(errors).toEqual([]);
});
