import { columnLabel, formatCell, finite, tone } from './model.js';
export function el(tag, text, className) {
  const node = document.createElement(tag);
  if (text != null) node.textContent = text;
  if (className) node.className = className;
  return node;
}
export function action(text, onClick, className) {
  const button = el('button', text, className); button.type = 'button'; button.onclick = onClick; return button;
}
export function metric(label, value, key = '', signed = false) {
  const item = el('div', null, 'metric');
  item.append(el('span', label, 'muted'), el('strong', formatCell(value, key), signed ? tone(value) : ''));
  return item;
}
export function detailList(value) {
  const list = el('dl', null, 'detail-list');
  for (const [key, item] of Object.entries(value || {})) {
    const dd = el('dd');
    if (item && typeof item === 'object') {
      const details = el('details'); details.append(el('summary', Array.isArray(item) ? `${item.length} items` : `${Object.keys(item).length} fields`), detailList(item)); dd.append(details);
    } else dd.textContent = formatCell(item, key);
    list.append(el('dt', columnLabel(key)), dd);
  }
  return list;
}
export function rangeBar(ohlc, last) {
  if (![ohlc?.low, ohlc?.high, last].every(finite) || ohlc.low > ohlc.high) return null;
  const block = el('section', null, 'range-section'); block.append(el('h3', 'Day’s range'));
  const labels = el('div', null, 'range-labels');
  labels.append(metric('Low', ohlc.low, 'low'), metric('High', ohlc.high, 'high')); block.append(labels);
  const bar = el('div', null, 'range-track');
  const position = price => ohlc.high === ohlc.low ? 50 : Math.max(0, Math.min(100, (price - ohlc.low) / (ohlc.high - ohlc.low) * 100));
  const marker = el('span', null, 'range-marker'); marker.style.left = `${position(last)}%`;
  marker.title = `Last price ${formatCell(last, 'last_price')}`;
  bar.setAttribute('role', 'img'); bar.setAttribute('aria-label', `Day low ${formatCell(ohlc.low, 'low')}, high ${formatCell(ohlc.high, 'high')}, last price ${formatCell(last, 'last_price')}${finite(ohlc.open) ? `, open ${formatCell(ohlc.open, 'open')}` : ''}.`);
  bar.append(marker);
  if (finite(ohlc.open)) { const open = el('span', null, 'range-open'); open.style.left = `${position(ohlc.open)}%`; open.title = `Open ${formatCell(ohlc.open, 'open')}`; bar.append(open); }
  block.append(bar, el('p', 'Solid marker: last price · Hollow marker: open', 'muted'));
  return block;
}
