export const allowedTools = new Set(['portfolio', 'market']);
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
const preferred = ['instrument', 'tradingsymbol', 'exchange', 'view', 'quantity', 't1_quantity', 'average_price', 'last_price', 'close', 'pnl', 'net_change', 'timestamp', 'date', 'open', 'high', 'low', 'volume', 'field', 'value'];
export function columnsFor(rows) {
  const keys = [...new Set(rows.flatMap(row => Object.keys(row)))];
  return [...preferred.filter(key => keys.includes(key)), ...keys.filter(key => !preferred.includes(key))];
}
export function visibleRows(rows, filter, view, sortKey, direction) {
  const query = filter.toLowerCase();
  return rows.filter(row => (!row.view || row.view === view) && JSON.stringify(row).toLowerCase().includes(query))
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
export function chartPoints(rows) {
  return rows.filter(row => typeof row.close === 'number' && Number.isFinite(row.close))
    .map(row => ({ close: row.close, date: row.date ?? row.timestamp ?? '' }));
}
export function formatValue(value) {
  if (value == null) return '—';
  if (typeof value === 'number') return new Intl.NumberFormat('en-IN', { maximumFractionDigits: 4 }).format(value);
  if (typeof value === 'object') return JSON.stringify(value, null, 2);
  return String(value);
}
