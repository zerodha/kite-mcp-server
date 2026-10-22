// A deterministic MCP Apps host exercises the real, bundled SDK in an iframe.
import { chromium } from 'playwright';
import { readFile, mkdir } from 'node:fs/promises';
import assert from 'node:assert/strict';
const html = await readFile(new URL('../app.html', import.meta.url), 'utf8');
const browser = await chromium.launch({ headless: true, ...(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {}), args: ['--no-sandbox'] });
const page = await browser.newPage({ viewport: { width: 900, height: 800 } });
const failures = [];
page.on('pageerror', error => failures.push(error.message));
const network = [];
page.on('request', request => { if (/^https?:/.test(request.url())) network.push(request.url()); });
try {
 await page.setContent('<!doctype html><html><body style="margin:0"><iframe title="Kite app" sandbox="allow-scripts" style="width:100%;height:740px;border:0"></iframe></body></html>');
 await page.evaluate(html => {
  const frame = document.querySelector('iframe');
  const tool = { name: 'portfolio', inputSchema: { type: 'object' } };
  window.calls = []; window.summaryCalls = []; window.panelCalls = [];
  window.send = (method, params) => frame.contentWindow.postMessage({ jsonrpc: '2.0', method, params }, '*');
  const result = (args = { mode: 'holdings', from: 0, limit: 2 }) => ({
   content: [{ type: 'text', text: 'Holdings snapshot' }],
   structuredContent: { data: args.from ? [{ tradingsymbol: 'TCS', exchange: 'NSE', quantity: 2 }] : [{ tradingsymbol: 'INFY', exchange: 'NSE', quantity: 11 }, { tradingsymbol: '<img src=x onerror=alert(1)>', exchange: 'BSE', quantity: 3 }], pagination: { from: args.from || 0, limit: 2, total: 3, returned: args.from ? 1 : 2, has_more: !args.from } },
   _meta: { 'trade.kite/app': { tool: 'portfolio', arguments: args } },
  });
  window.fixture = result;
  window.addEventListener('message', event => {
   if (event.source !== frame.contentWindow || event.data?.jsonrpc !== '2.0') return;
   const m = event.data;
   if (m.method === 'ui/initialize') frame.contentWindow.postMessage({ jsonrpc:'2.0', id:m.id, result:{ protocolVersion:m.params.protocolVersion, hostInfo:{name:'Kite test host',version:'1'}, hostCapabilities:{serverTools:{}}, hostContext:{theme:'light',toolInfo:{tool}} } }, '*');
   else if (m.method === 'ui/notifications/initialized') {
    window.send('ui/notifications/tool-input', { arguments:{mode:'holdings',from:0,limit:2} });
    window.send('ui/notifications/tool-result', result());
   } else if (m.method === 'tools/call') {
    if(m.params.name==='portfolio' && m.params.arguments.type==='summary') {
     window.summaryCalls.push(m.params);
     frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result:{structuredContent:{invested_amount:100000,current_value:112000,total_pnl:12000,total_pnl_percent:12,today_pnl:-300,today_pnl_percent:-.27},_meta:{'trade.kite/app':{tool:'portfolio',arguments:m.params.arguments}}}},'*');
     return;
    }
    window.calls.push(m.params);
    const response = window.rejectNext ? { content:[{type:'text',text:'Login required'}],isError:true } : result(m.params.arguments);
    window.rejectNext = false;
    const message = window.rpcErrorNext
     ? {jsonrpc:'2.0',id:m.id,error:{code:-32000,message:'Bridge unavailable'}}
     : {jsonrpc:'2.0',id:m.id,result:response};
    window.rpcErrorNext = false;
    const respond = () => frame.contentWindow.postMessage(message, '*');
    if (window.echoInputNext) {
     window.echoInputNext = false;
     window.send('ui/notifications/tool-input', {arguments:m.params.arguments});
    }
    if (window.deferNext) { window.deferNext = false; window.release = respond; }
    else respond();
   }
  });
  frame.srcdoc = html;
 }, html);
 const app = page.frameLocator('iframe');
 await app.locator('tbody tr').first().waitFor();
 assert.equal(await app.locator('tbody tr').count(), 2);
 assert.equal(await app.locator('td img').count(), 0, 'upstream strings must not become HTML');
 await app.getByRole('button', { name: 'Settled qty', exact: true }).click();
 assert.match(await app.locator('tbody tr').first().textContent(), /BSE/);
 assert.equal(await app.locator('thead button:focus').count(), 1, 'sort retains keyboard focus');
 assert.equal(await app.locator('caption').isVisible(), true, 'caption remains in the accessibility tree');
 await app.getByRole('searchbox').fill('INFY');
 assert.equal(await app.locator('tbody tr').count(), 1);
 await app.getByRole('searchbox').fill('');
 await app.getByRole('button', { name: 'Next', exact: true }).click();
 await app.getByText('Rows 3–3 of 3', {exact:false}).waitFor();
 assert.equal((await page.evaluate(() => window.calls.at(-1))).arguments.from, 2);
 await app.getByRole('button', { name: 'Refresh', exact: true }).click();
 await page.waitForFunction(() => window.calls.length === 2);
 assert.equal((await page.evaluate(() => window.calls.at(-1))).arguments.from, 2);
 await page.evaluate(() => { window.rejectNext = true; });
 await app.getByRole('button', { name: 'Refresh', exact: true }).click();
 await app.getByText('Login required', {exact:true}).waitFor();
 assert.equal(await app.locator('tbody tr').count(), 0);
 assert.equal(await app.getByRole('button', {name:'Retry',exact:true}).isEnabled(), true);
 // Switch the initial tool result to positions: net is the default, day is separate.
 await page.evaluate(() => {
  window.send('ui/notifications/tool-input', {arguments:{mode:'positions'}});
  window.send('ui/notifications/tool-result', {structuredContent:{data:[{tradingsymbol:'INFY',view:'net',quantity:3},{tradingsymbol:'INFY',view:'day',quantity:1}]},_meta:{'trade.kite/app':{tool:'portfolio',arguments:{mode:'positions'}}}});
 });
 await app.locator('#view-label').waitFor();
 assert.equal(await app.locator('tbody tr').count(), 1);
 assert.match(await app.locator('tbody tr').textContent(), /3/);
 await app.locator('#view').selectOption('day');
 assert.match(await app.locator('tbody tr').textContent(), /1/);
 // A late bridge response must not replace a newer host request or unlock it.
 await page.evaluate(() => { window.deferNext = true; });
 await app.getByRole('button', { name: 'Refresh', exact: true }).click();
 await page.waitForFunction(() => typeof window.release === 'function');
 await page.evaluate(() => window.send('ui/notifications/tool-input', {arguments:{mode:'margins'}}));
 await app.getByRole('heading', {name:'Funds & margins'}).waitFor();
 assert.equal(await app.locator('tbody tr').count(), 0, 'old snapshot must be cleared on a new request');
 await app.getByText('Loading snapshot', {exact:true}).waitFor();
 assert.equal(await app.locator('#content').getByText('Loading snapshot',{exact:true}).isVisible(),true);
 assert.equal(await app.getByRole('button',{name:'Refresh',exact:true}).isDisabled(),true);
 await page.evaluate(() => window.release());
 await page.waitForTimeout(100);
 assert.equal(await app.locator('tbody tr').count(), 0, 'late bridge result must not overwrite the new request');
 assert.equal(await app.getByRole('button', {name:'Refresh',exact:true}).isDisabled(), true);
 await page.evaluate(() => window.send('ui/notifications/tool-cancelled', {reason:'Cancelled by user'}));
 await app.getByText('Request cancelled. Refresh to retry.', {exact:true}).waitFor();
 assert.equal(await app.getByRole('button', {name:'Refresh',exact:true}).isEnabled(), true);
 // Late result notifications must not revive a cancelled request.
 await page.evaluate(() => window.send('ui/notifications/tool-result', window.fixture({mode:'margins'})));
 await page.waitForTimeout(100);
 assert.equal(await app.locator('tbody tr').count(), 0);
 // Retrying works even when a host echoes tool-input before the call response.
 await page.evaluate(() => { window.echoInputNext = true; });
 await app.getByRole('button', {name:'Refresh',exact:true}).click();
 await app.locator('tbody tr').first().waitFor();
 // A bridge error after echoed input must leave Refresh available for retry.
 await page.evaluate(() => { window.echoInputNext = true; window.rpcErrorNext = true; });
 await app.getByRole('button', {name:'Refresh',exact:true}).click();
 await app.locator('#status').filter({hasText:'Bridge unavailable'}).waitFor();
 assert.equal(await app.getByRole('button', {name:'Refresh',exact:true}).isEnabled(), true);
 // Historical results produce both an accessible chart and a full data table.
 await page.evaluate(() => {
  window.send('ui/notifications/tool-input', {arguments:{mode:'historical',instrument_token:123,interval:'day',from_date:'2026-10-20',to_date:'2026-10-21'}});
  window.send('ui/notifications/tool-result', {structuredContent:{data:[{date:'2026-10-20',open:99,close:100},{date:'2026-10-21',open:100,close:110}],meta:{timezone:'Asia/Kolkata'}},_meta:{'trade.kite/app':{tool:'market',arguments:{mode:'historical',instrument_token:123,interval:'day',from_date:'2026-10-20',to_date:'2026-10-21'}}}});
 });
 await app.getByRole('img').waitFor();
 assert.equal(await app.locator('tbody tr').count(), 2);
 await mkdir(new URL('../test-artifacts/', import.meta.url), {recursive:true});
 await page.screenshot({path:new URL('../test-artifacts/chart-light.png',import.meta.url).pathname});
 await page.setViewportSize({width:390,height:800});
 await page.evaluate(() => window.send('ui/notifications/host-context-changed',{theme:'dark'}));
 await page.waitForTimeout(100);
 const overflow = await app.locator('body').evaluate(node => node.scrollWidth > window.innerWidth);
 assert.equal(overflow, false, 'narrow viewport should scroll tables, not the whole document');
 await page.screenshot({path:new URL('../test-artifacts/chart-dark-mobile.png',import.meta.url).pathname});
 assert.equal(await app.locator('html').evaluate(node=>node.style.colorScheme), 'dark');
 // Theme updates retain initial host toolInfo. Refresh must still call market.
 await page.evaluate(() => { window.rejectNext = true; });
 await app.getByRole('button', {name:'Refresh',exact:true}).click();
 await app.getByText('Login required', {exact:true}).waitFor();
 const lastCall = await page.evaluate(()=>window.calls.at(-1));
 assert.equal(lastCall.name, 'market');
 assert.equal(lastCall.arguments.instrument_token, 123);
 assert.equal(lastCall.arguments.mode, 'historical');
 // Real-shaped rows exercise financial formatting, additional columns and nested details.
 await page.setViewportSize({width:900,height:1000});
 await page.locator('iframe').evaluate(frame => frame.style.height = '960px');
 await page.evaluate(() => {
  window.send('ui/notifications/host-context-changed',{theme:'light'});
  window.send('ui/notifications/tool-input', {arguments:{mode:'holdings'}});
  window.send('ui/notifications/tool-result', {structuredContent:{data:[
   {tradingsymbol:'INFY',exchange:'NSE',quantity:25,average_price:1450.5,last_price:1523.8,pnl:1832.5,t1_quantity:0,ohlc:{open:1500,high:1530,low:1490,close:1510}},
   {tradingsymbol:'TCS',exchange:'NSE',quantity:10,average_price:3600,last_price:3540.25,pnl:-597.5,t1_quantity:2},
   {tradingsymbol:'HDFCBANK',exchange:'NSE',quantity:40,average_price:1700,last_price:null,pnl:0,t1_quantity:0}
  ],meta:{missing_instruments:['NSE:UNKNOWN']}},_meta:{'trade.kite/app':{tool:'portfolio',arguments:{mode:'holdings'}}}});
 });
 await app.getByRole('heading',{name:'Holdings',exact:true}).waitFor();
 await app.locator('.data-table').getByText('+1,832.50',{exact:true}).waitFor();
 assert.equal(await app.locator('td.negative').count(), 1);
 assert.equal(await app.locator('#warning').isVisible(), true);
 await app.getByRole('button',{name:'More columns',exact:true}).click();
 await app.locator('.data-table summary').first().click();
 assert.match(await app.locator('.data-table details[open]').textContent(), /1,530.00/);
 await page.screenshot({path:new URL('../test-artifacts/holdings-light.png',import.meta.url).pathname});
 await page.setViewportSize({width:390,height:1000});
 assert.equal(await app.locator('body').evaluate(node=>node.scrollWidth > window.innerWidth), false);
 await page.screenshot({path:new URL('../test-artifacts/holdings-mobile.png',import.meta.url).pathname});
 await page.evaluate(() => window.send('ui/notifications/tool-input',{arguments:{mode:'margins'}}));
 await app.getByText('Loading snapshot',{exact:true}).waitFor();
 await page.screenshot({path:new URL('../test-artifacts/loading-mobile.png',import.meta.url).pathname});
 await page.evaluate(() => window.send('ui/notifications/tool-result',{isError:true,content:[{type:'text',text:'Login required'}]}));
 await app.getByRole('button',{name:'Retry',exact:true}).waitFor();
 assert.equal(await app.getByRole('button',{name:'Retry',exact:true}).isEnabled(), true);
 await page.screenshot({path:new URL('../test-artifacts/error-mobile.png',import.meta.url).pathname});
 assert.deepEqual(network, [], 'app must not request any network resources');
 assert.deepEqual(failures, []);
 console.log('Browser checks passed: SDK handshake, tables, sorting, filtering, pagination, refresh, errors, stale responses, cancellation/retry, net/day positions, chart, theme-safe tool selection, mobile layout, escaped content, no network requests.');
} finally { await browser.close(); }
