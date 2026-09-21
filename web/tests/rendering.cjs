// Real WebGL map/2D chart interactions, with local libraries and synthetic GPS.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const root = path.join(__dirname, '../..');
const read = name => fs.readFileSync(path.join(root, name), 'utf8');
const partial = name => read(`web/templates/partials/${name}.html`).replace(/{{define [^}]+}}|{{end}}/g, '');
const points = Array.from({ length: 9 }, (_, i) => ({
  lat: 44.80 + i * 0.002, lng: 20.44 + i * 0.003, time: `2026-09-20T09:0${i}:00Z`,
  altitude: 80 + i * 10, speed: 3 + i, heartrate: 100 + i * 10, cadence: 70 + i,
  moving: i !== 4, grade: i - 3
}));
const zones = { heart_rate: { zones: [{ min: 0, max: 120 }, { min: 120, max: 140 }, { min: 140, max: 160 }, { min: 160, max: 180 }, { min: 180, max: -1 }] } };
const graph = () => Object.fromEntries(['speed', 'heartrate', 'height', 'cadence'].map(metric => [metric,
  points.map((p, i) => ({ time: p.time, value: p[metric === 'height' ? 'altitude' : metric], distance: i * 1000, zone: Math.min(5, 1 + Math.floor(i / 2)) }))
]));
const modal = read('web/templates/activity.html').match(/<div id="segment-modal"[\s\S]*?<\/body>/)[0].replace('</body>', '');
const html = `<meta charset="utf-8"><link rel="stylesheet" href="/vendor/map.css"><link rel="stylesheet" href="/static/app.css">
<style>body{padding:10px;display:block}#map{height:350px;width:850px}#graph-container{height:350px;width:850px}#graph-canvas{max-height:250px}.map-shell{width:850px}</style>
<script src="/vendor/map.js"></script><script src="/vendor/chart.js"></script><script src="/vendor/date.js"></script>
<script>
window.__MAP_STYLE_URL__='/fixture-style.json'; window.mapErrors=[];
const RealMap=maplibregl.Map;
maplibregl.Map=class extends RealMap { constructor(options){super({...options,fadeDuration:0});window.testMap=this;this.on('error',e=>mapErrors.push(e.error.message));} };
Chart.defaults.animation=false;
</script>
${partial('map').replace('{{template "color_controls" .}}', partial('color_controls'))}
<button id="create-segment-btn">Create Segment</button>
${partial('graph').replace(/{{if hasActivity .}}([\s\S]*?){{else}}[\s\S]*?(?=<\/div>)/, '$1')}${modal}`;

module.exports = function register(fixture) {
  async function activity(t, setup, pathname = '/activity/42', extraHTML = '') {
    const page = await fixture(t, html + extraHTML, async page => {
      const assets = {
        '/vendor/map.js': ['node_modules/maplibre-gl/dist/maplibre-gl.js', 'text/javascript'],
        '/vendor/map.css': ['node_modules/maplibre-gl/dist/maplibre-gl.css', 'text/css'],
        '/vendor/chart.js': ['node_modules/chart.js/dist/chart.umd.js', 'text/javascript'],
        '/vendor/date.js': ['node_modules/chartjs-adapter-date-fns/dist/chartjs-adapter-date-fns.bundle.js', 'text/javascript']
      };
      await page.route('**/vendor/*', route => {
        const [file, contentType] = assets[new URL(route.request().url()).pathname];
        return route.fulfill({ body: read(file), contentType });
      });
      await page.route('**/static/app.css', route => route.fulfill({ body: read('web/static/app.css'), contentType: 'text/css' }));
      await page.route('**/static/icons/*.svg', route => route.fulfill({ body: read('web' + new URL(route.request().url()).pathname), contentType: 'image/svg+xml' }));
      await page.route('**/fixture-style.json', route => route.fulfill({ json: { version: 8, sources: {}, layers: [{ id: 'background', type: 'background', paint: { 'background-color': '#19212b' } }] } }));
      await page.route('**/api/activities/42/points', route => route.fulfill({ json: points }));
      await page.route('**/api/activities/42/graph?*', route => route.fulfill({ json: graph() }));
      await page.route('**/api/hrzones', route => route.fulfill({ json: zones }));
      if (setup) await setup(page);
    }, pathname);
    return page;
  }
  async function ready(page) {
    await page.waitForFunction(() => testMap.getLayer('route-points-layer') && Chart.getChart('graph-canvas') && testMap.loaded());
    assert.deepEqual(await page.evaluate(() => mapErrors), [], 'MapLibre style/rendering errors');
  }
  async function clickPoint(page, index) {
    await page.locator('#map').scrollIntoViewIfNeeded();
    await page.waitForFunction(() => !testMap.isMoving() && testMap.loaded());
    const p = await page.evaluate(point => {
      const p = testMap.project([point.lng, point.lat]);
      const r = testMap.getCanvas().getBoundingClientRect();
      return { x: r.left + p.x, y: r.top + p.y };
    }, points[index]);
    await page.mouse.click(p.x, p.y);
  }

  test('real map renders a delayed route, endpoints, and metric colors', async t => {
    const page = await activity(t, async page => {
      await page.route('**/api/activities/42/points', async route => {
        // Force points to arrive after load, as with a slow backend and cached style.
        await page.waitForFunction(() => window.testMap?.loaded());
        await route.fulfill({ json: points });
      });
    });
    await ready(page);
    assert.equal(await page.evaluate(() => testMap.queryRenderedFeatures({ layers: ['route-plain-line'] }).length > 0), true);
    assert.equal(await page.evaluate(() => testMap.hasImage('route-marker-start') && testMap.hasImage('route-marker-finish')), true);
    await page.locator('#color-metric').selectOption('speed');
    assert.match(await page.locator('#legend').textContent(), /Speed.*39.6/s);
    assert.equal(await page.evaluate(() => testMap.getPaintProperty('route-plain-line', 'line-opacity')), 0);
    await page.locator('#color-metric').selectOption('hrzones');
    await page.waitForFunction(() => document.querySelector('#legend').textContent.includes('Z5'));
    assert.match(await page.locator('#legend').textContent(), /≥ 180 bpm/);
    await page.locator('#color-metric').selectOption('moving');
    assert.equal(await page.locator('#legend').isVisible(), false);
    await page.locator('#color-metric').selectOption('none');
    assert.equal(await page.evaluate(() => testMap.getPaintProperty('route-points-layer', 'circle-opacity')), 0);
    assert.deepEqual(await page.evaluate(() => mapErrors), []);
  });

  test('map clicks select chart samples on both axes after metric changes', async t => {
    const page = await activity(t);
    await ready(page);
    await page.locator('#color-metric').selectOption('speed');
    await clickPoint(page, 3);
    await page.waitForFunction(() => Chart.getChart('graph-canvas').getActiveElements()[0]?.index === 3);
    assert.match(await page.locator('.maplibregl-popup-content').textContent(), /HR: 130/);
    await page.locator('#metric1-select').selectOption('speed');
    await page.locator('#metric2-select').selectOption('cadence');
    await page.locator('#xaxis-select').selectOption('distance');
    await page.waitForFunction(() => {
      const c = Chart.getChart('graph-canvas');
      return c?.scales.x.type === 'linear' && c.data.datasets.length === 2;
    });
    assert.deepEqual(await page.evaluate(() => Chart.getChart('graph-canvas').data.datasets.map(d => d.data[3].y)), [21.6, 73]);
    // Count updates caused by one real pointer click to catch accumulated map listeners.
    await page.evaluate(() => {
      const c = Chart.getChart('graph-canvas'); const update = c.update.bind(c);
      window.selectionUpdates = 0;
      c.update = (...args) => { if (args[0] === 'none') selectionUpdates++; return update(...args); };
    });
    await clickPoint(page, 6);
    await page.waitForFunction(() => Chart.getChart('graph-canvas').getActiveElements()[0]?.index === 6);
    assert.equal(await page.evaluate(() => selectionUpdates), 1);
    await page.locator('#graph-canvas').scrollIntoViewIfNeeded();
    const graphPoint = await page.evaluate(() => {
      const chart = Chart.getChart('graph-canvas');
      const point = chart.getDatasetMeta(0).data[6];
      const rect = chart.canvas.getBoundingClientRect();
      return { x: rect.left + point.x, y: rect.top + point.y };
    });
    await page.mouse.move(graphPoint.x, graphPoint.y);
    await page.waitForFunction(() => Chart.getChart('graph-canvas').tooltip.title?.[0] === 'Distance: 6.00 km');
    assert.match(await page.evaluate(() => Chart.getChart('graph-canvas').tooltip.body[0].lines.join(' ')), /Speed: 32.4 km\/h/);
    await page.locator('#metric1-select').selectOption('');
    await page.locator('#metric2-select').selectOption('');
    assert.equal(await page.locator('#graph-placeholder').isVisible(), true);
    assert.equal(await page.evaluate(() => !!Chart.getChart('graph-canvas')), false);
  });

  test('map segment selection orders endpoints, preserves failed saves, and submits exclusive finish', async t => {
    const requests = [];
    const page = await activity(t, async page => {
      await page.route('**/api/segments', route => {
        requests.push(route.request().postDataJSON());
        return requests.length === 1 ? route.fulfill({ status: 503, body: 'Please retry' }) : route.fulfill({ json: { id: 99 } });
      });
    });
    await ready(page);
    await page.locator('#create-segment-btn').click();
    await clickPoint(page, 6);
    await clickPoint(page, 6);
    assert.match(await page.locator('#segment-step-copy').textContent(), /different point/);
    await clickPoint(page, 2);
    assert.match(await page.locator('#segment-summary').textContent(), /4m 00s/);
    await page.waitForFunction(() => testMap.queryRenderedFeatures({ layers: ['segment-preview'] }).length > 0);
    await page.locator('#segment-save-panel-btn').click();
    await page.locator('#segment-name').fill(' River climb ');
    await page.locator('#segment-description').fill(' Two bridges ');
    page.once('dialog', async dialog => { assert.match(dialog.message(), /Please retry/); await dialog.accept(); });
    await page.locator('#segment-submit-btn').click();
    await page.waitForFunction(() => document.querySelector('#segment-modal').style.display === 'flex');
    assert.equal(await page.locator('#segment-name').inputValue(), ' River climb ');
    await page.locator('#segment-submit-btn').click();
    await page.waitForURL('**/segment/99');
    assert.deepEqual(requests, Array(2).fill({ name: 'River climb', description: 'Two bridges', activity_id: 42, start_index: 2, end_index: 7 }));
  });

  test('an older graph response cannot replace the latest metric or revive a hidden chart', async t => {
    const held = [];
    const page = await activity(t);
    await ready(page);
    await page.route('**/api/activities/42/graph?*', route => { held.push(route); });
    await page.locator('#metric1-select').selectOption('speed');
    await page.locator('#metric1-select').selectOption('cadence');
    await page.waitForFunction(() => document.querySelector('#metric1-select').value === 'cadence');
    const deadline = Date.now() + 5000;
    while (held.length < 2 && Date.now() < deadline) await new Promise(r => setTimeout(r, 10));
    assert.equal(held.length, 2);
    await held[1].fulfill({ json: graph() });
    await page.waitForFunction(() => Chart.getChart('graph-canvas')?.data.datasets[0].label === 'Cadence');
    await held[0].fulfill({ json: graph() });
    await page.waitForLoadState('networkidle');
    assert.equal(await page.evaluate(() => Chart.getChart('graph-canvas').data.datasets[0].label), 'Cadence');
    await page.locator('#metric1-select').selectOption('speed');
    while (held.length < 3 && Date.now() < deadline) await new Promise(r => setTimeout(r, 10));
    await page.locator('#metric1-select').selectOption('');
    await held[2].fulfill({ json: graph() });
    await page.waitForLoadState('networkidle');
    assert.equal(await page.evaluate(() => !!Chart.getChart('graph-canvas')), false);
    assert.equal(await page.locator('#graph-placeholder').isVisible(), true);
  });

  test('graph fetch failure is visible and a new selection recovers', async t => {
    const page = await activity(t, async page => {
      await page.route('**/api/activities/42/graph?*', route => route.fulfill({ status: 503, body: 'temporarily unavailable' }));
    });
    await page.waitForFunction(() => document.querySelector('#graph-placeholder').textContent.includes('Unable to load'));
    assert.match(await page.locator('#graph-placeholder').textContent(), /Unable to load|Could not load/i);
    await page.route('**/api/activities/42/graph?*', route => route.fulfill({ json: graph() }));
    await page.locator('#metric1-select').selectOption('speed');
    await ready(page);
    assert.equal(await page.locator('#graph-placeholder').isVisible(), false);
  });

  test('late HR zones cannot overwrite a newly selected route color', async t => {
    let held;
    const page = await activity(t);
    await ready(page);
    await page.route('**/api/hrzones', route => { held = route; });
    const requested = page.waitForRequest('**/api/hrzones');
    await page.locator('#color-metric').selectOption('hrzones');
    await requested;
    await page.locator('#color-metric').selectOption('speed');
    await held.fulfill({ json: zones });
    await page.waitForLoadState('networkidle');
    assert.match(await page.locator('#legend').textContent(), /Speed/);
    assert.doesNotMatch(await page.locator('#legend').textContent(), /Z1/);
    assert.deepEqual(await page.evaluate(() => mapErrors), []);
  });
  test('segment comparisons align effort samples and clearing selection removes map and chart', async t => {
    const sidebar = partial('segment_sidebar').replace(/{{[^}]+}}/g, '');
    const page = await activity(t, async page => {
      await page.addInitScript(() => { window.__SEGMENT_ID__ = 7; });
      await page.route('**/api/segments/7', async route => {
        await page.waitForFunction(() => window.testMap?.loaded());
        await route.fulfill({ json: { segment_geog: `LINESTRING(${points.map(p => `${p.lng} ${p.lat}`).join(',')})` } });
      });
      await page.route('**/api/segments/7/metrics', route => route.fulfill({ json: { distance: 8000, elevation_gain: 80 } }));
      await page.route('**/api/segments/7/activities?*', route => route.fulfill({ json: [
        { id: 42, name: 'River ride', start_date: '2026-09-20', segment_elapsed_seconds: 240, segment_avg_speed: 6 },
        { id: 43, name: 'Earlier ride', start_date: '2026-09-19', segment_elapsed_seconds: 300, segment_avg_speed: 5 }
      ] }));
      await page.route('**/api/activities/*/points', route => route.fulfill({ json: points }));
      await page.route('**/api/segments/7/activity/*/indices?*', route => route.fulfill({ json: { start_index: 2, end_index: 6 } }));
      await page.route('**/api/segments/7/graph?*', route => route.fulfill({ json: Object.fromEntries(Object.entries(graph()).map(([k, values]) => [k, values.slice(2, 7)])) }));
    }, '/segment/7', sidebar);
    await page.waitForFunction(() => testMap.getLayer('segment-line') && testMap.loaded());
    assert.equal(await page.locator('#segment-distance').textContent(), '8.00 km');
    await page.locator('.compare-toggle[data-activity-id="42"]').click();
    await page.waitForFunction(() => Chart.getChart('graph-canvas')?.data.datasets.length === 1 && testMap.getLayer('comparison-effort-line-0'));
    await page.locator('.compare-toggle[data-activity-id="43"]').click();
    await page.waitForFunction(() => Chart.getChart('graph-canvas')?.data.datasets.length === 2 && testMap.getLayer('comparison-effort-line-1'));
    assert.deepEqual(await page.evaluate(() => Chart.getChart('graph-canvas').data.datasets.map(d => d.data.map(p => p.x))), [ [0,60,120,180,240], [0,60,120,180,240] ]);
    await page.locator('#xaxis-select').selectOption('distance');
    await page.waitForFunction(() => Chart.getChart('graph-canvas')?.data.datasets[0].data[1].x === 1);
    assert.deepEqual(await page.evaluate(() => mapErrors), []);
    await page.locator('.compare-toggle[data-activity-id="42"]').click();
    await page.waitForFunction(() => Chart.getChart('graph-canvas')?.data.datasets.length === 1);
    await page.locator('.compare-toggle[data-activity-id="43"]').click();
    assert.equal(await page.evaluate(() => !!Chart.getChart('graph-canvas')), false);
    assert.equal(await page.evaluate(() => !!testMap.getLayer('comparison-effort-line-0')), false);

    // Finish a request after its selection was removed: it must not resurrect it.
    const held = [];
    await page.route('**/api/segments/7/graph?*', route => { held.push([route, graph()]); });
    await page.route('**/api/activities/*/points', route => { held.push([route, points]); });
    await page.locator('.compare-toggle[data-activity-id="42"]').click();
    const deadline = Date.now() + 5000;
    while (held.length < 2 && Date.now() < deadline) await new Promise(r => setTimeout(r, 10));
    assert.equal(held.length, 2);
    await page.locator('.compare-toggle[data-activity-id="42"]').click();
    await Promise.all(held.map(([route, json]) => route.fulfill({ json })));
    await page.waitForLoadState('networkidle');
    assert.equal(await page.evaluate(() => !!Chart.getChart('graph-canvas')), false);
    assert.equal(await page.evaluate(() => !!testMap.getLayer('comparison-effort-line-0')), false);
    assert.equal(await page.locator('#graph-placeholder').isVisible(), true);
    assert.deepEqual(await page.evaluate(() => mapErrors), []);
  });

};
