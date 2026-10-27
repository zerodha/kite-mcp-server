export const allowedTools = new Set(['portfolio', 'market', 'orders_read', 'gtt_read']);
// Argument object key order can differ between host notifications and Go JSON.
export function requestKey(value) {
  function ordered(item) {
    if (Array.isArray(item)) return item.map(ordered);
    if (item && typeof item === 'object') return Object.fromEntries(Object.keys(item).sort().map(key => [key, ordered(item[key])]));
    return item;
  }
  return JSON.stringify(ordered(value));
}
export function readResult(result) {
  if (result.isError) throw new Error(result.content?.filter(x => x.type === 'text').map(x => x.text).join('\n') || 'Kite could not complete this request.');
  if (result.structuredContent != null) return result.structuredContent;
  const text = result.content?.find(x => x.type === 'text')?.text;
  if (!text) throw new Error('No data was returned.');
  return JSON.parse(text);
}
export function rowsFor(payload) {
  const data = payload && Object.hasOwn(payload, 'data') ? payload.data : payload;
  if (Array.isArray(data)) return data.map(x => x && typeof x === 'object' ? x : { value: x });
  if (data && typeof data === 'object') {
    const entries = Object.entries(data);
    // Quote maps are keyed by exchange:symbol, not a single flat record.
    if (entries.length && entries.every(([key, value]) => key.includes(':') && value && typeof value === 'object')) {
      return entries.map(([instrument, value]) => ({ instrument, ...value }));
    }
    return entries.map(([field, value]) => ({ field, value }));
  }
  return data == null ? [] : [{ value: data }];
}
const preferred = ['instrument', 'tradingsymbol', 'exchange', 'product', 'view', 'quantity', 't1_quantity', 'average_price', 'last_price', 'close', 'pnl', 'net_change', 'timestamp', 'date', 'open', 'high', 'low', 'volume', 'field', 'value'];
export function columnsFor(rows) {
  const keys = [...new Set(rows.flatMap(row => Object.keys(row)))];
  const order = keys.includes('date') && keys.includes('close') ? ['date', 'timestamp', 'open', 'high', 'low', 'close', 'volume'] : preferred;
  return [...order.filter(key => keys.includes(key)), ...keys.filter(key => !order.includes(key))];
}
export function visibleRows(rows, filter, view, sortKey, direction) {
  const query = filter.trim().toLowerCase();
  return rows.filter(row => (!row.view || row.view === view) && searchableValues(row).some(value => value.toLowerCase().includes(query)))
    .sort((a, b) => {
      if (!sortKey) return 0;
      const x = a[sortKey], y = b[sortKey];
      if (x == null) return y == null ? 0 : 1;
      if (y == null) return -1;
      return direction * (typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y)));
    });
}
export function pageArguments(args, pagination, step) {
  return { ...args, from: Math.max(0, pagination.from + step * pagination.limit), limit: pagination.limit };
}
export function formatValue(value) {
  if (value == null) return '—';
  if (typeof value === 'number') return new Intl.NumberFormat('en-IN', { maximumFractionDigits: 4 }).format(value);
  if (typeof value === 'object') return JSON.stringify(value, null, 2);
  return String(value);
}

const labels = { tradingsymbol: 'Symbol', instrument: 'Instrument', quantity: 'Qty', t1_quantity: 'T1 qty', average_price: 'Avg. price', last_price: 'Last price', pnl: 'P&L', net_change: 'Change', instrument_token: 'Instrument ID', ohlc: 'OHLC' };
export function columnLabel(key) {
  return labels[key] || key.replaceAll('_', ' ').replace(/\b\w/g, c => c.toUpperCase());
}
export function isMoney(key) {
  return /price|pnl|profit|margin|balance|cash|debit|credit|^open$|^high$|^low$|^close$|net_change|^realised$|^unrealised$|^m2m$|^strike$|^value$|invested_amount|current_value/.test(key) && !/quantity|timestamp|percent|percentage/.test(key);
}
export function isSigned(key) { return /pnl|profit|net_change|^price_change$/.test(key); }
export function isPrice(key) { return /price|^open$|^high$|^low$|^close$|^strike$|tick_size|circuit|^net_change$|^day_change$/.test(key) && !/percent|percentage/.test(key); }
export function formatCell(value, key = '') {
  if (value == null || value === '' || (typeof value === 'number' && !Number.isFinite(value))) return '—';
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  if (typeof value === 'number') {
    const percent = /percent|percentage/.test(key), money = !percent && isMoney(key);
    return new Intl.NumberFormat('en-IN', { minimumFractionDigits: money || percent ? 2 : 0, maximumFractionDigits: percent ? 2 : isPrice(key) ? 4 : money ? 2 : 4, signDisplay: isSigned(key) ? 'exceptZero' : 'auto' }).format(value) + (percent ? '%' : '');
  }
  return formatValue(value);
}

// Search exactly the strings used by cells and expanded details, never object keys.
export function searchableValues(row) {
  function values(value, key) {
    if (Array.isArray(value)) return value.flatMap(item => values(item, key));
    if (value && typeof value === 'object') return Object.entries(value).flatMap(([field, item]) => values(item, field));
    return [formatCell(value, key)];
  }
  return Object.entries(row).flatMap(([key, value]) => values(value, key === 'value' && row.field ? row.field : key));
}
export const finite = value => typeof value === 'number' && Number.isFinite(value);
export const tone = value => finite(value) && value !== 0 ? value > 0 ? 'positive' : 'negative' : '';
export function instrumentRef(row) {
  const id = [row.instrument, row.id].find(value => typeof value === 'string' && value.includes(':'));
  const [exchange, ...symbol] = id ? id.split(':') : [row.exchange, row.tradingsymbol];
  if (!exchange || !symbol.join(':')) return null;
  return { id: `${exchange}:${symbol.join(':')}`, exchange, symbol: symbol.join(':'), token: Number(row.instrument_token) > 0 ? Number(row.instrument_token) : null };
}
export function rowIdentity(row) {
  return JSON.stringify([instrumentRef(row)?.id, row.product || '', row.view || '']);
}
export function expiryFor(row) {
  const expiry = row.expiry_date || row.expiry;
  return typeof expiry === 'string' && !expiry.startsWith('0001') && !expiry.startsWith('1970-01-01') ? expiry.slice(0, 10) : '';
}
// quantity is settled stock; t1_quantity is held separately by Kite Connect.
// https://kite.trade/docs/connect/v3/portfolio/#response-attributes
export function holdingQuantity(row) {
  return finite(row.quantity) ? row.quantity + (finite(row.t1_quantity) ? row.t1_quantity : 0) : null;
}
export function optionStrike(row) { return ['CE','PE'].includes(row.instrument_type) && finite(row.strike) && row.strike > 0; }
export function holdingMetrics(row) {
  const quantity = holdingQuantity(row);
  const invested = finite(quantity) && finite(row.average_price) ? quantity * row.average_price : null;
  const current = finite(quantity) && finite(row.last_price) ? quantity * row.last_price : null;
  const percent = invested > 0 && finite(row.pnl) ? row.pnl / invested * 100 : null;
  return { invested, current, percent };
}
export function dateIST(value = new Date()) {
  return new Intl.DateTimeFormat('en-CA', { timeZone:'Asia/Kolkata', year:'numeric', month:'2-digit', day:'2-digit' }).format(new Date(value));
}
export function receivedTime(value = new Date()) {
  return new Intl.DateTimeFormat('en-IN', { timeZone:'Asia/Kolkata', hour:'2-digit', minute:'2-digit', second:'2-digit' }).format(new Date(value)) + ' IST';
}
export function historyArguments(token, from, to, interval = 'day') {
  if (!Number.isInteger(token) || token <= 0) throw new Error('An instrument token is required for charts.');
  const validDate=value=>/^\d{4}-\d{2}-\d{2}$/.test(value)&&Number.isFinite(Date.parse(value))&&new Date(value).toISOString().slice(0,10)===value;
  if (!validDate(from) || !validDate(to) || from > to) throw new Error('Choose a valid start date on or before the end date.');
  return { mode:'historical', instrument_token:token, from_date:from, to_date:to, interval };
}

export function snapshotTime(value) {
  if (!value || String(value).startsWith('0001')) return '';
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return String(value);
  return new Intl.DateTimeFormat('en-IN', { timeZone:'Asia/Kolkata', day:'2-digit', month:'short', hour:'2-digit', minute:'2-digit', second:'2-digit' }).format(date) + ' IST';
}
