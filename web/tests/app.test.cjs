const { test, before, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const script = fs.readFileSync(path.join(__dirname, '../static/app.js'), 'utf8');
let browser;
const coverage = [];
before(async () => { browser = await chromium.launch({ headless: true, args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader'] }); });
after(async () => {
  await browser?.close();
  // Union V8's covered source ranges over all browser scenarios. Offsets count
  // UTF-16 code units (JavaScript string positions), not encoded file bytes.
  // This covers the complete app.js, not statements/branches or backend code.
  const hit = new Uint8Array(script.length);
  for (const entry of coverage) {
    const ranges = entry.functions.flatMap(fn => fn.ranges).sort((a, b) => (b.endOffset - b.startOffset) - (a.endOffset - a.startOffset));
    const run = new Uint8Array(script.length);
    for (const r of ranges) run.fill(r.count > 0 ? 1 : 0, r.startOffset, r.endOffset);
    run.forEach((value, i) => { if (value) hit[i] = 1; });
  }
  const covered = hit.reduce((a, b) => a + b, 0);
  const result = { file: 'web/static/app.js', metric: 'V8 source ranges (UTF-16 code units)', covered, total: script.length, percent: 100 * covered / script.length, scenarios: coverage.length };
  const dir = process.env.COVERAGE_DIR || path.join(__dirname, '../../coverage/frontend');
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, 'summary.json'), JSON.stringify(result, null, 2) + '\n');
  fs.writeFileSync(path.join(dir, 'v8.json'), JSON.stringify(coverage));
  console.log(`app.js browser coverage: ${result.percent.toFixed(1)}% of source text (${result.scenarios} scenarios)`);
});

async function fixture(t, html, setup, pathname = '/') {
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.coverage.startJSCoverage({ resetOnNavigation: false });
  // Fail closed: fixtures never reach Strava, a user's backend, or map providers.
  await page.route('**/*', route => {
    if (route.request().url() === 'http://b11k.test/static/app.js') return route.fulfill({ contentType: 'text/javascript', body: script });
    if (route.request().resourceType() === 'document') return route.fulfill({ contentType: 'text/html', body: html + '<script src="/static/app.js"></script>' });
    return route.abort();
  });
  if (setup) await setup(page);
  t.after(async () => {
    coverage.push(...(await page.coverage.stopJSCoverage()).filter(x => x.url.endsWith('/static/app.js')));
    await page.close();
    assert.deepEqual(errors, [], 'uncaught browser errors');
  });
  await page.goto('http://b11k.test' + pathname);
  page.setDefaultTimeout(7000);
  return page;
}

const syncHTML = `<form id="sync-form"><input name="start"><input name="end"><button>Sync</button></form>
<pre id="sync-log"></pre><div id="sync-progress"><div id="progress-bar-container"><div id="progress-bar"></div></div>
<span id="progress-text"></span><span id="progress-phase"></span></div>`;
async function eventSource(page) {
  await page.addInitScript(() => {
    window.streams = [];
    window.EventSource = class extends EventTarget {
      constructor(url) { super(); this.url = url; this.closed = false; window.streams.push(this); }
      close() { this.closed = true; }
      emit(type, data) {
        const event = data === undefined ? new Event(type) : new MessageEvent(type, { data: JSON.stringify(data) });
        this.dispatchEvent(event);
        if (type === 'error' && this.onerror) this.onerror(event);
      }
    };
  });
}
async function emit(page, type, data) {
  await page.evaluate(({ type, data }) => window.streams.at(-1).emit(type, data), { type, data });
}

test('sync sends optional dates and recognizes durable import phases', async t => {
  const page = await fixture(t, syncHTML, eventSource);
  await page.locator('[name=start]').fill('2026-09-01');
  await page.locator('button').click();
  assert.equal(await page.evaluate(() => streams[0].url), '/strava/sync?start=2026-09-01');
  await emit(page, 'progress', { phase: 'importing', current: 2, total: 4, message: '' });
  assert.equal(await page.locator('#progress-bar').evaluate(e => e.style.width), '50%');
  assert.match(await page.locator('#progress-phase').textContent(), /Importing/);
});

test('discovery shows found activities without a false percentage', async t => {
  const page = await fixture(t, syncHTML, eventSource);
  await page.locator('button').click();
  await emit(page, 'progress', { phase: 'discovering', current: 200, total: 200 });
  assert.doesNotMatch(await page.locator('#progress-text').textContent(), /100%/);
  assert.match(await page.locator('#progress-text').textContent(), /200.*found/i);
  await emit(page, 'progress', { phase: 'finalizing', current: 200, total: 200, message: 'Updating map' });
  assert.match(await page.locator('#progress-phase').textContent(), /map/i);
});

test('repeated submit closes previous progress stream', async t => {
  const page = await fixture(t, syncHTML, eventSource);
  await page.locator('button').click();
  await page.locator('button').click();
  assert.deepEqual(await page.evaluate(() => streams.map(s => s.closed)), [true, false]);
});

test('server error remains visible and closes its stream', async t => {
  const page = await fixture(t, syncHTML, eventSource);
  await page.locator('button').click();
  await emit(page, 'error', 'Reconnect Strava, then retry this sync.');
  assert.match(await page.locator('#sync-log').textContent(), /Reconnect Strava/);
  assert.equal(await page.evaluate(() => streams[0].closed), true);
});

test('network interruption explains saved progress instead of undefined error', async t => {
  const page = await fixture(t, syncHTML, eventSource);
  await page.locator('button').click();
  await emit(page, 'error');
  assert.doesNotMatch(await page.locator('#sync-log').textContent(), /undefined/);
  assert.match(await page.locator('#sync-log').textContent(), /server|saved/i);
});

const segmentsHTML = `<input id="segments-filter"><select id="segments-direction"><option>all</option><option>uphill</option></select>
<select id="segments-sort"><option>name</option><option>attempts</option></select><div id="segments-dashboard">
${[[1, 'river', 'flat', 2], [2, 'hill', 'uphill', 7]].map(([id, name, direction, attempts]) => `<article class="segment-card" data-segment-id="${id}" data-name="${name}" data-direction="${direction}" data-attempts="${attempts}"><button class="delete-segment-btn" data-segment-id="${id}" data-segment-name="${name}">Delete ${name}</button></article>`).join('')}</div>
<div id="delete-modal" style="display:none"><span id="delete-segment-name"></span><button id="delete-cancel-btn">Cancel</button><button id="delete-confirm-btn">Confirm</button></div>`;

test('segment filtering, direction and sorting work together', async t => {
  const page = await fixture(t, segmentsHTML);
  await page.locator('#segments-sort').selectOption('attempts');
  assert.deepEqual(await page.locator('.segment-card').evaluateAll(xs => xs.map(x => x.dataset.name)), ['hill', 'river']);
  await page.locator('#segments-filter').fill(' RIVER ');
  assert.deepEqual(await page.locator('.segment-card:visible').evaluateAll(xs => xs.map(x => x.dataset.name)), ['river']);
  await page.locator('#segments-direction').selectOption('uphill');
  assert.equal(await page.locator('.segment-card:visible').count(), 0);
  await page.locator('#segments-filter').fill('');
  assert.deepEqual(await page.locator('.segment-card:visible').evaluateAll(xs => xs.map(x => x.dataset.name)), ['hill']);
});

test('cancelled deletion sends no request; failed deletion retains the card', async t => {
  let deletes = 0;
  const page = await fixture(t, segmentsHTML, async page => {
    await page.route('**/api/segments/*', route => { deletes++; return route.fulfill({ status: 503, body: 'Try again' }); });
  });
  await page.getByText('Delete river', { exact: true }).click();
  await page.locator('#delete-cancel-btn').click();
  assert.equal(deletes, 0);
  await page.getByText('Delete river', { exact: true }).click();
  const pendingDialog = page.waitForEvent('dialog');
  const click = page.locator('#delete-confirm-btn').click();
  const dialog = await pendingDialog;
  assert.match(dialog.message(), /Try again/);
  await dialog.dismiss();
  await click;
  assert.equal(deletes, 1);
  assert.equal(await page.locator('article[data-segment-id="1"]').count(), 1);
});

test('successful deletion removes the card and filtering cannot resurrect it', async t => {
  const page = await fixture(t, segmentsHTML, async page => {
    await page.route('**/api/segments/*', route => route.fulfill({ status: 204 }));
  });
  await page.getByText('Delete river', { exact: true }).click();
  await page.locator('#delete-confirm-btn').click();
  await page.locator('article[data-segment-id="1"]').waitFor({ state: 'detached' });
  await page.locator('#segments-filter').fill('river');
  assert.equal(await page.locator('.segment-card:visible').count(), 0);
});

test('changing page size keeps filters and resets page number', async t => {
  const page = await fixture(t, '<p>Activities</p>');
  await page.goto('http://b11k.test/?page=8&q=river&sport=Ride');
  await page.evaluate(() => window.changePerPage('50'));
  await page.waitForURL('**/*per_page=50*');
  const url = new URL(page.url());
  assert.equal(url.searchParams.get('page'), '1');
  assert.equal(url.searchParams.get('q'), 'river');
  assert.equal(url.searchParams.get('sport'), 'Ride');
});

test('quota waits show the server reason and stale streams cannot overwrite progress', async t => {
  const page = await fixture(t, syncHTML, eventSource);
  await page.locator('button').click();
  await page.locator('button').click();
  await emit(page, 'progress', { phase: 'importing', state: 'waiting', current: 2, total: 4, message: 'Waiting for Strava quota reset.' });
  await page.evaluate(() => streams[0].emit('progress', { phase: 'saving', current: 4, total: 4 }));
  assert.equal(await page.locator('#progress-phase').textContent(), 'Waiting to retry');
  assert.match(await page.locator('#progress-text').textContent(), /quota reset/);
});

test('completed sync closes and reloads the library', async t => {
  const page = await fixture(t, syncHTML, eventSource);
  await page.locator('button').click();
  const reloaded = page.waitForEvent('load');
  await emit(page, 'summary', { success: 2, failed: 0 });
  await emit(page, 'done', 'ok');
  await reloaded;
  assert.equal(await page.locator('#sync-log').textContent(), '');
});

const discoveredHTML = '<div id="discovered-map"></div><p id="discovered-status"></p><button id="discovered-rebuild-btn">Rebuild</button>';
async function mapFixture(page) {
  await page.addInitScript(() => {
    window.__MAP_STYLE_URL__ = '/fixture-map.json';
    window.maplibregl = { Map: class {
      constructor() { this.handlers = {}; this.sources = {}; window.testMap = this; setTimeout(() => this.fire('load'), 0); }
      on(name, handler) { (this.handlers[name] ||= []).push(handler); }
      fire(name) { return Promise.all((this.handlers[name] || []).map(fn => fn())); }
      addSource(name, source) { this.sources[name] = { data: source.data, setData(data) { this.data = data; } }; }
      getSource(name) { return this.sources[name]; }
      addLayer() {}
      fitBounds(bounds) { this.fitted = bounds; }
      getBounds() { return { getWest: () => 20, getEast: () => 21, getSouth: () => 44, getNorth: () => 45 }; }
    } };
  });
}

test('discovered map reports stale coverage, fits data, and recovers from failed rebuild', async t => {
  const page = await fixture(t, discoveredHTML, async page => {
    await mapFixture(page);
    await page.route('**/api/discovered/**', route => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith('/rebuild')) return route.fulfill({ status: 503, body: 'Rebuild unavailable' });
      const value = url.pathname.endsWith('/status') ? { stale: true, message: 'Coverage needs a refresh', bbox: [20, 44, 21, 45] } : { type: 'FeatureCollection', features: [] };
      return route.fulfill({ json: value });
    });
  });
  await page.waitForFunction(() => document.querySelector('#discovered-status').textContent.includes('needs a refresh'));
  assert.deepEqual(await page.evaluate(() => testMap.fitted), [[20, 44], [21, 45]]);
  await page.locator('#discovered-rebuild-btn').click();
  await page.waitForFunction(() => document.querySelector('#discovered-status').textContent === 'Rebuild unavailable');
  assert.equal(await page.locator('#discovered-rebuild-btn').isEnabled(), true);
  assert.match(await page.locator('#discovered-status').getAttribute('class'), /warning/);
});

test('late map requests cannot replace newer viewport coverage', async t => {
  let hold = false;
  const held = [];
  const page = await fixture(t, discoveredHTML, async page => {
    await mapFixture(page);
    await page.route('**/api/discovered/**', route => {
      if (route.request().url().includes('/status')) return route.fulfill({ json: { stale: false, cached_activities: 1 } });
      if (hold) { held.push(route); return; }
      return route.fulfill({ json: { type: 'FeatureCollection', features: [], marker: 'new viewport' } });
    });
  });
  await page.waitForFunction(() => testMap.getSource('discovered-coverage')?.data.marker === 'new viewport');
  hold = true;
  await page.evaluate(() => { window.pendingViewport = testMap.fire('moveend'); });
  const deadline = Date.now() + 5000;
  while (held.length < 2 && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 10));
  assert.equal(held.length, 2);
  hold = false;
  await page.evaluate(() => testMap.fire('moveend'));
  await Promise.all(held.map(route => route.fulfill({ json: { type: 'FeatureCollection', features: [], marker: 'old viewport' } })));
  await page.evaluate(() => window.pendingViewport);
  assert.equal(await page.evaluate(() => testMap.getSource('discovered-coverage').data.marker), 'new viewport');
});

require('./rendering.cjs')(fixture);
