import { test } from 'node:test';
import assert from 'node:assert/strict';
import { requestKey, readResult, rowsFor, visibleRows, pageArguments } from '../src/model.js';

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

test('financial display uses fixed decimals and search matches displayed values, not keys', async () => {
  const { formatCell, columnsFor } = await import('../src/model.js');
  assert.equal(formatCell(1234.5, 'last_price'), '1,234.50');
  assert.equal(formatCell(-10, 'pnl'), '-10.00');
  assert.equal(formatCell(10, 'pnl'), '+10.00');
  assert.equal(formatCell(null, 'last_price'), '—');
  assert.equal(formatCell('', 'exchange'), '—');
  const rows = [{ exchange:'NSE', last_price:1234.5 }];
  assert.equal(visibleRows(rows, 'exchange', 'net').length, 0);
  assert.equal(visibleRows(rows, '1,234.50', 'net').length, 1);
  assert.deepEqual(columnsFor([{close:100,date:'2026-01-01',open:90}]), ['date','open','close']);
});

test('search uses the exact field/value and nested display formatting without matching hidden keys', async () => {
  const { formatCell, instrumentRef, expiryFor, historyArguments } = await import('../src/model.js');
  assert.equal(visibleRows([{field:'cash',value:1234.5}],'1,234.50','net').length,1);
  assert.equal(visibleRows([{ohlc:{open:1234.5}}],'1,234.50','net').length,1);
  assert.equal(visibleRows([{ohlc:{open:1234.5}}],'open','net').length,0);
  assert.equal(formatCell(12,'total_pnl_percent'),'+12.00%');
  assert.deepEqual(instrumentRef({id:'NFO:NIFTY26OCT25000CE',instrument_token:123}),{id:'NFO:NIFTY26OCT25000CE',exchange:'NFO',symbol:'NIFTY26OCT25000CE',token:123});
  assert.equal(expiryFor({expiry_date:'0001-01-01'}),'');
  assert.throws(()=>historyArguments(123,'2026-10-23','2026-10-01'),/start date/);
});

test('four-decimal price ticks survive while portfolio amounts stay at two decimals', async()=>{
  const {formatCell,holdingMetrics,holdingQuantity,optionStrike}=await import('../src/model.js');
  assert.equal(formatCell(83.2475,'last_price'),'83.2475');
  assert.equal(formatCell(83.25,'last_price'),'83.25');
  assert.equal(formatCell(.0125,'last_price'),'0.0125');
  assert.equal(formatCell(.0025,'price_change'),'+0.0025');
  assert.equal(formatCell(1234.5678,'invested_amount'),'1,234.57');
  assert.equal(holdingQuantity({quantity:0,t1_quantity:10}),10);
  assert.deepEqual(holdingMetrics({quantity:0,t1_quantity:10,average_price:100,last_price:150,pnl:500}),{invested:1000,current:1500,percent:50});
  assert.deepEqual(holdingMetrics({quantity:10,t1_quantity:10,average_price:100,last_price:150,pnl:1000}),{invested:2000,current:3000,percent:50});
  assert.equal(optionStrike({instrument_type:'FUT',strike:0}),false);
  assert.equal(optionStrike({instrument_type:'CE',strike:25000}),true);
});

test('historical dates reject impossible calendar dates instead of normalizing them',async()=>{
  const {historyArguments}=await import('../src/model.js');
  assert.throws(()=>historyArguments(111,'2026-02-30','2026-03-05'),/valid start date/);
  assert.equal(historyArguments(111,'2024-02-29','2024-03-01').from_date,'2024-02-29');
});
