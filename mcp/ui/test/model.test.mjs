import { test } from 'node:test';
import assert from 'node:assert/strict';
import { requestKey, readResult, rowsFor, visibleRows, pageArguments, chartPoints } from '../src/model.js';

test('structured and legacy text results render the same rows; errors stay errors', () => {
  const payload = { data: [{ tradingsymbol: 'INFY', quantity: 3 }], pagination: { total: 1 } };
  assert.deepEqual(readResult({ structuredContent: payload }), readResult({ content: [{ type: 'text', text: JSON.stringify(payload) }] }));
  assert.throws(() => readResult({ isError: true, structuredContent: payload, content: [{ type: 'text', text: 'Login required' }] }), /Login required/);
  assert.deepEqual(rowsFor({ data: { 'NSE:INFY': { last_price: 100 } } }), [{ instrument: 'NSE:INFY', last_price: 100 }]);
  assert.deepEqual(rowsFor({ data: null, meta: {} }), []);
});

test('request identity ignores object key order without ignoring array order', () => {
  assert.equal(requestKey({ mode: 'quote', nested: { b: 2, a: 1 } }), requestKey({ nested: { a: 1, b: 2 }, mode: 'quote' }));
  assert.notEqual(requestKey({ instruments: ['NSE:INFY', 'NSE:TCS'] }), requestKey({ instruments: ['NSE:TCS', 'NSE:INFY'] }));
});

test('net and day positions never combine, and sorting is numeric', () => {
  const rows = [{ view: 'net', quantity: 11, tradingsymbol: 'INFY' }, { view: 'day', quantity: 9, tradingsymbol: 'INFY' }, { view: 'net', quantity: 2, tradingsymbol: 'TCS' }];
  assert.deepEqual(visibleRows(rows, '', 'net', 'quantity', 1).map(x => x.quantity), [2, 11]);
  assert.deepEqual(visibleRows(rows, 'infy', 'day').map(x => x.quantity), [9]);
  assert.equal(rows.length, 3);
});

test('page navigation retains mode, query and filters', () => {
  const args = { mode: 'search', query: 'INFY', exchange: 'NSE', include_live_quote: true };
  assert.deepEqual(pageArguments(args, { from: 100, limit: 100 }, 1), { ...args, from: 200, limit: 100 });
  assert.equal(pageArguments(args, { from: 0, limit: 100 }, -1).from, 0);
});

test('chart uses finite closing prices and preserves candle dates', () => {
  assert.deepEqual(chartPoints([{ date: '2026-01-01', close: 100 }, { close: null }, { close: Infinity }]), [{ date: '2026-01-01', close: 100 }]);
});
