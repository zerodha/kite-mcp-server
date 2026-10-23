import { chromium } from 'playwright';
import { readFile } from 'node:fs/promises';
import assert from 'node:assert/strict';

const html = await readFile(new URL('../app.html', import.meta.url), 'utf8');
const browser = await chromium.launch({ headless:true, ...(process.env.CHROMIUM_PATH ? {executablePath:process.env.CHROMIUM_PATH} : {}), args:['--no-sandbox'] });
try {
 for(const withToolInfo of [true,false]) {
 const page = await browser.newPage();
 page.setDefaultTimeout(10000);
 const failures=[];
 page.on('pageerror', error=>failures.push(error.message));
 await page.setContent('<iframe sandbox="allow-scripts" title="Kite app" style="width:900px;height:800px"></iframe>');
 await page.evaluate(({html,withToolInfo})=>{
  const frame=document.querySelector('iframe');
  window.calls=[];
  window.send=(method,params)=>frame.contentWindow.postMessage({jsonrpc:'2.0',method,params},'*');
  window.launch=()=>{const result=window.snapshot({mode:'holdings',type:'full',from:0,limit:100});result._meta['trade.kite/app'].origin={tool:'kite_explorer',arguments:{}};return result;};
  window.snapshot=(args)=>({structuredContent:{data:[{tradingsymbol:'INFY',exchange:'NSE',quantity:3}]},_meta:{'trade.kite/app':{tool:'portfolio',arguments:args}}});
  window.addEventListener('message',event=>{
   if(event.source!==frame.contentWindow)return;
   const m=event.data;
   if(m.method==='ui/initialize')frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result:{protocolVersion:m.params.protocolVersion,hostInfo:{name:'Test',version:'1'},hostCapabilities:{serverTools:{}},hostContext:withToolInfo?{toolInfo:{tool:{name:'kite_explorer',inputSchema:{type:'object'}}}}:{}}},'*');
   if(m.method==='ui/notifications/initialized')window.send('ui/notifications/tool-input',{arguments:{}});
   if(m.method==='tools/call'){
    window.calls.push(m.params);
    frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result:window.snapshot(m.params.arguments)},'*');
   }
  });
  frame.srcdoc=html;
 },{html,withToolInfo});
 const app=page.frameLocator('iframe');
 await app.getByText('Loading snapshot',{exact:true}).waitFor();
 // Untagged snapshots cannot satisfy the launcher input, with or without toolInfo.
 await page.evaluate(()=>window.send('ui/notifications/tool-result',window.snapshot({mode:'holdings',type:'full',from:0,limit:100})));
 await page.waitForTimeout(100);
 assert.equal(await app.getByText('Loading snapshot',{exact:true}).isVisible(),true);
 await page.evaluate(()=>window.send('ui/notifications/tool-result',window.launch()));
 await app.getByRole('heading',{name:'Holdings',exact:true}).waitFor();
 await app.locator('tbody tr').first().waitFor();
 assert.match(await app.locator('tbody').textContent(),/INFY/);
 assert.equal(await app.getByRole('button',{name:'Refresh',exact:true}).isEnabled(),true);
 await app.getByRole('button',{name:'Refresh',exact:true}).click();
 await page.waitForFunction(()=>window.calls.some(call=>call.arguments.type==='full'));
 const refresh=await page.evaluate(()=>window.calls.find(call=>call.arguments.type==='full'));
 assert.equal(refresh.name,'portfolio');
 assert.deepEqual(refresh.arguments,{mode:'holdings',type:'full',from:0,limit:100});
 // Reopening with an authentication failure must render the error and allow retry.
 await page.evaluate(()=>{
  window.send('ui/notifications/tool-input',{arguments:{}});
  window.send('ui/notifications/tool-result',{isError:true,content:[{type:'text',text:'Login required'}],_meta:{'trade.kite/app':{tool:'portfolio',arguments:{mode:'holdings',type:'full',from:0,limit:100},origin:{tool:'kite_explorer',arguments:{}}}}});
 });
 await app.getByText('Login required',{exact:true}).waitFor();
 assert.equal(await app.getByRole('button',{name:'Retry',exact:true}).isEnabled(),true);
 // A late launcher result must not satisfy a newer explicit portfolio request.
 await page.evaluate(()=>{
  window.send('ui/notifications/tool-input',{arguments:{mode:'positions'}});
  window.send('ui/notifications/tool-result',window.launch());
 });
 await app.getByText('Loading snapshot',{exact:true}).waitFor();
 await page.waitForTimeout(100);
 assert.equal(await app.getByText('Loading snapshot',{exact:true}).isVisible(),true);
 await page.evaluate(()=>window.send('ui/notifications/tool-result',window.snapshot({mode:'positions'})));
 await app.locator('tbody tr').first().waitFor();
 assert.deepEqual(failures,[]);
 await page.close();
 }
 console.log('Launcher checks passed: empty input, stale-result rejection, holdings, refresh, and authentication error.');
} finally {await browser.close();}
