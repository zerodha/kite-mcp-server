import { App, applyHostStyleVariables } from '@modelcontextprotocol/ext-apps';
import { allowedTools, requestKey, readResult, rowsFor, columnsFor, visibleRows, pageArguments, chartPoints, formatValue } from './model.js';

const $ = id => document.getElementById(id);
const app = new App({ name: 'Kite explorer', version: '1.0.0' }, {}, { autoResize: true });
let tool = '', args = {}, payload = null, rows = [], busy = false, connected = false, hasResult = false;
let sortKey = '', direction = 1, inFlight = false;
let revision = 0, waiting = false, expectedArgs = null, pendingCall = null, cancelled = false;
function status(text, error = false) {
  $('status').textContent = text;
  $('status').classList.toggle('error', error);
}
function setBusy(value) {
  busy = value || inFlight;
  $('content').setAttribute('aria-busy', String(busy));
  const canCall = connected && hasResult && allowedTools.has(tool) && Boolean(app.getHostCapabilities()?.serverTools);
  $('refresh').disabled = busy || !canCall;
  const page = payload?.pagination;
  $('previous').disabled = busy || !canCall || !page || page.from <= 0;
  $('next').disabled = busy || !canCall || !page?.has_more;
}
function hostContext(ctx) {
  if (ctx?.styles?.variables) applyHostStyleVariables(ctx.styles.variables);
  if (ctx?.theme) {
    document.documentElement.style.colorScheme = ctx.theme;
    // Explicit host colors take precedence over system dark-mode fallbacks.
    const dark = ctx.theme === 'dark';
    const defaults = { '--color-text-primary': dark ? '#e2e5ea' : '#252a34', '--color-text-secondary': dark ? '#a4aebf' : '#657083', '--color-background-primary': dark ? '#181b20' : '#fff', '--color-background-secondary': dark ? '#222730' : '#f6f7f9', '--color-border-primary': dark ? '#414957' : '#ccd1d9', '--color-border-secondary': dark ? '#343b47' : '#e8ebef' };
    for (const [key, value] of Object.entries(defaults)) if (!ctx.styles?.variables?.[key]) document.documentElement.style.setProperty(key, value);
  }
  const name = ctx?.toolInfo?.tool?.name;
  // A context/theme notification may retain the tool that first opened the
  // shared view. Result metadata is authoritative after that initial context.
  if (!tool && allowedTools.has(name)) tool = name;
}
function element(tag, text, className) {
  const node = document.createElement(tag);
  if (text != null) node.textContent = text;
  if (className) node.className = className;
  return node;
}
function drawChart(container) {
  if (tool !== 'market' || args.mode !== 'historical') return;
  const points = chartPoints(rows);
  if (points.length < 2) return;
  const prices = points.map(p => p.close);
  const low = Math.min(...prices), high = Math.max(...prices), range = high - low || 1;
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 800 220');
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', `Historical closing prices, ${points.length} candles. Low ${formatValue(low)}, high ${formatValue(high)}. Full values are in the table.`);
  svg.classList.add('chart');
  const polyline = document.createElementNS(svg.namespaceURI, 'polyline');
  polyline.setAttribute('points', points.map((p, i) => `${10 + i / (points.length - 1) * 780},${200 - (p.close - low) / range * 180}`).join(' '));
  polyline.setAttribute('fill', 'none');
  polyline.setAttribute('stroke', 'currentColor');
  polyline.setAttribute('stroke-width', '2');
  svg.append(polyline);
  const caption = element('div', null, 'chart-caption');
  caption.append(element('span', `${points[0].date} → ${points.at(-1).date}`), element('span', `Close · ${formatValue(low)} – ${formatValue(high)}`));
  container.append(svg, caption);
}
function render() {
  $('title').textContent = `${tool === 'market' ? 'Market' : 'Portfolio'} · ${String(args.mode || 'snapshot').replaceAll('_', ' ')}`;
  $('content').replaceChildren();
  const hasViews = rows.some(row => row.view === 'day' || row.view === 'net');
  $('controls').hidden = rows.length === 0;
  $('view-label').hidden = !hasViews;
  drawChart($('content'));
  const visible = visibleRows(rows, $('filter').value, $('view').value, sortKey, direction);
  if (!visible.length) $('content').append(element('div', rows.length ? 'No matching rows on this page.' : 'No data returned for this request.', 'empty'));
  else {
    const columns = columnsFor(rows);
    const table = element('table');
    const caption = element('caption', 'Kite snapshot data');
    caption.hidden = true;
    table.append(caption);
    const head = element('thead'), header = element('tr');
    for (const key of columns) {
      const th = element('th');
      th.scope = 'col';
      th.setAttribute('aria-sort', sortKey === key ? direction === 1 ? 'ascending' : 'descending' : 'none');
      const button = element('button', key.replaceAll('_', ' ') + (sortKey === key ? direction === 1 ? ' ↑' : ' ↓' : ''));
      button.onclick = () => { direction = sortKey === key ? -direction : 1; sortKey = key; render(); };
      th.append(button); header.append(th);
    }
    head.append(header); table.append(head);
    const body = element('tbody');
    for (const row of visible) {
      const tr = element('tr');
      for (const key of columns) {
        const value = row[key], td = element('td');
        if (typeof value === 'number') td.className = 'numeric';
        if (value && typeof value === 'object') td.append(element('pre', formatValue(value)));
        else td.textContent = formatValue(value);
        tr.append(td);
      }
      body.append(tr);
    }
    table.append(body);
    const scroll = element('div', null, 'scroll'); scroll.tabIndex = 0;
    scroll.setAttribute('role', 'region'); scroll.setAttribute('aria-label', 'Scrollable snapshot table');
    scroll.append(table); $('content').append(scroll);
  }
  const page = payload?.pagination;
  $('pagination').hidden = !page;
  if (page) $('page-label').textContent = page.returned
    ? `Rows ${page.from + 1}–${page.from + page.returned} of ${page.total} · ${visible.length} visible`
    : `No rows on this page · ${page.total} total`;
  const notes = [];
  if (hasViews) notes.push('Net shows current exposure; Day shows today’s activity. The views are separate and must not be added together. The selector filters this page.');
  if (payload?.meta?.missing_instruments?.length) notes.push(`Missing instruments: ${payload.meta.missing_instruments.join(', ')}.`);
  if (payload?.meta?.timestamp_note) notes.push(payload.meta.timestamp_note);
  if (payload?.meta?.timezone) notes.push(`Timezone: ${payload.meta.timezone}.`);
  if (tool === 'market' && args.mode === 'historical') notes.push('Chart shows closing prices at equal candle spacing. The table contains full OHLC and volume values.');
  $('note').textContent = notes.join(' ');
  setBusy(busy);
}
function receive(result, fallbackArgs) {
  if (cancelled) return;
  const info = result._meta?.['trade.kite/app'];
  // Ignore delayed host notifications belonging to a superseded request.
  if (info && expectedArgs && requestKey(info.arguments || {}) !== requestKey(expectedArgs)) return;
  if (info && allowedTools.has(info.tool)) { tool = info.tool; args = info.arguments || {}; }
  else if (fallbackArgs) args = fallbackArgs;
  waiting = false;
  try {
    payload = readResult(result);
    rows = rowsFor(payload);
    hasResult = true;
    render();
    status(`Snapshot received at ${new Date().toLocaleTimeString()}. ${rows.length} rows on this page.`);
  } catch (error) {
    payload = null; rows = []; hasResult = true;
    render(); status(error.message, true);
  }
  setBusy(false);
}
async function call(nextArgs) {
  if (busy || !connected || !allowedTools.has(tool) || !app.getHostCapabilities()?.serverTools) return;
  const currentRevision = ++revision;
  const requestedTool = tool;
  expectedArgs = nextArgs;
  const operation = { arguments: nextArgs, controller: new AbortController() };
  pendingCall = operation;
  cancelled = false;
  inFlight = true; setBusy(true); status('Requesting data from Kite…');
  try {
    const result = await app.callServerTool({ name: requestedTool, arguments: nextArgs }, { signal: operation.controller.signal });
    if (revision === currentRevision) receive(result, nextArgs);
  } catch (error) {
    if (revision === currentRevision) {
      waiting = false;
      hasResult = true;
      expectedArgs = args;
      status(`Could not refresh: ${error.message}. Any displayed data is the previous snapshot.`, true);
    }
  } finally {
    if (pendingCall === operation) {
      pendingCall = null;
      inFlight = false; setBusy(waiting);
    }
  }
}
$('refresh').onclick = () => call({ ...args });
$('previous').onclick = () => payload?.pagination && call(pageArguments(args, payload.pagination, -1));
$('next').onclick = () => payload?.pagination && call(pageArguments(args, payload.pagination, 1));
$('filter').oninput = render;
$('view').onchange = render;
app.ontoolinput = params => {
  const nextArgs = params.arguments || {};
  const ownCall = pendingCall && requestKey(nextArgs) === requestKey(pendingCall.arguments);
  if (!ownCall) {
    ++revision;
    pendingCall?.controller.abort();
  }
  cancelled = false;
  expectedArgs = nextArgs;
  args = nextArgs;
  payload = null; rows = []; hasResult = false; waiting = true;
  if (!ownCall) {
    $('filter').value = ''; $('view').value = 'net'; sortKey = ''; direction = 1;
  }
  render(); setBusy(true); status('Waiting for Kite data…');
};
app.ontoolresult = result => receive(result);
app.ontoolcancelled = () => {
  ++revision; waiting = false; hasResult = true; cancelled = true;
  pendingCall?.controller.abort();
  status('Request cancelled. Refresh to retry.', true); setBusy(false);
};
app.onhostcontextchanged = ctx => hostContext({ ...app.getHostContext(), ...ctx });
app.onteardown = async () => {
  ++revision; connected = false; waiting = false;
  pendingCall?.controller.abort();
  setBusy(false); return {};
};
app.connect().then(() => {
  connected = true;
  hostContext(app.getHostContext());
  setBusy(busy);
}).catch(error => {
  status(`Could not connect to the MCP Apps host: ${error.message}`, true); setBusy(false);
});
