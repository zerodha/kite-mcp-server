import { chromium } from 'playwright';
import { readFile, mkdir } from 'node:fs/promises';
import assert from 'node:assert/strict';
const html=await readFile(new URL('../app.html',import.meta.url),'utf8');
const browser=await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH?{executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
const page=await browser.newPage({viewport:{width:1180,height:1050}});
const failures=[],network=[];
page.on('pageerror',error=>failures.push(error.message));
page.on('request',request=>{if(/^https?:/.test(request.url()))network.push(request.url());});
await mkdir(new URL('../test-artifacts/',import.meta.url),{recursive:true});
const capture=name=>page.screenshot({path:new URL(`../test-artifacts/${name}.png`,import.meta.url).pathname});
try{
 await page.setContent('<!doctype html><html><body style="margin:0"><iframe title="Kite explorer" sandbox="allow-scripts" style="width:100%;height:1000px;border:0"></iframe></body></html>');
 await page.evaluate(html=>{
  const frame=document.querySelector('iframe');
  window.send=(method,params)=>frame.contentWindow.postMessage({jsonrpc:'2.0',method,params},'*');
  window.calls=[];window.responses=[];window.echoPanels=true;window.echoMain=true;
  window.holdings=[
   {tradingsymbol:'INFY',exchange:'NSE',instrument_token:111,product:'CNC',quantity:25,average_price:1450.5,last_price:1523.8,pnl:1832.5,day_change:13.8,day_change_percentage:.91},
   {tradingsymbol:'TCS',exchange:'NSE',instrument_token:222,product:'CNC',quantity:10,average_price:3600,last_price:3540.25,pnl:-597.5,day_change:-12.5},
   {tradingsymbol:'HDFCBANK',exchange:'NSE',instrument_token:333,product:'CNC',quantity:40,average_price:1700,last_price:1750,pnl:2000}
  ];
  window.contracts=[
   {id:'NFO:NIFTY26OCT25000CE',tradingsymbol:'NIFTY26OCT25000CE',exchange:'NFO',instrument_token:444,name:'NIFTY',instrument_type:'CE',expiry_date:'2026-10-27',strike:25000,lot_size:65,active:true},
   {id:'NFO:NIFTY26OCT25000PE',tradingsymbol:'NIFTY26OCT25000PE',exchange:'NFO',instrument_token:445,name:'NIFTY',instrument_type:'PE',expiry_date:'2026-10-27',strike:25000,lot_size:65,active:true},
   {id:'NFO:NIFTY26NOVFUT',tradingsymbol:'NIFTY26NOVFUT',exchange:'NFO',instrument_token:446,name:'NIFTY',instrument_type:'FUT',strike:0,expiry_date:'2026-11-24',lot_size:65,active:true}
  ];
  window.companies=[{id:'NSE:INFY',tradingsymbol:'INFY',exchange:'NSE',instrument_token:111,name:'Infosys Ltd',instrument_type:'EQ',strike:0,lot_size:1,expiry_date:''}];
  window.wrap=(name,args,structuredContent)=>({structuredContent,_meta:{'trade.kite/app':{tool:name,arguments:args}}});
  window.emit=(name,args,data)=>{window.mainName=name;window.mainArgs=args;window.send('ui/notifications/tool-input',{arguments:args});window.send('ui/notifications/tool-result',window.wrap(name,args,data));};
  const pageData=from=>({data:from?[window.holdings[2]]:window.holdings.slice(0,2),pagination:{from:from||0,limit:2,total:3,returned:from?1:2,has_more:!from}});
  const fixture=(name,args)=>{
   if(name==='portfolio'&&args.type==='summary')return {invested_amount:200000,current_value:225000,total_pnl:25000,total_pnl_percent:12.5,today_pnl:-1000,today_pnl_percent:-.44};
   if(name==='portfolio')return pageData(args.from);
   if(args.mode==='search'){
    const matches=[...window.contracts,...window.companies].filter(row=>String(row[args.filter_on||'id']||'').toLowerCase().includes(args.query.toLowerCase()));
    return {data:matches,pagination:{from:0,limit:100,total:matches.length,returned:matches.length,has_more:false},meta:{timestamp_note:'Instrument master snapshot'}};
   }
   if(args.mode==='quote'){
    const id=args.instruments[0],token=id.includes('TCS')?222:id.includes('NIFTY')?444:111;
    const price=token===222?3540.25:token===444?125.5:1523.8;
    return {data:{[id]:{instrument_token:token,last_price:price,ohlc:{open:price-10,high:price+15,low:price-25,close:price-8},average_price:price-2,volume:235600,oi:token===444?560000:0,last_quantity:25,last_trade_time:'2026-10-23T10:30:00+05:30',timestamp:'2026-10-23T10:30:01+05:30',buy_quantity:15000,sell_quantity:18000,lower_circuit_limit:price*.9,upper_circuit_limit:price*1.1,depth:{buy:Array.from({length:5},(_,i)=>({price:price-.05*(i+1),quantity:250+i*100,orders:2+i})),sell:Array.from({length:5},(_,i)=>({price:price+.05*(i+1),quantity:300+i*125,orders:3+i}))}}},meta:{timezone:'Asia/Kolkata'}};
   }
   if(args.mode==='historical')return {data:Array.from({length:25},(_,i)=>{const open=1500+i*2+Math.sin(i)*12,close=open+Math.sin(i+1)*8;return {date:new Date(Date.parse(args.from_date)+i*86400000).toISOString().slice(0,10),open,close,high:Math.max(open,close)+5,low:Math.min(open,close)-6,volume:10000+i*200,oi:0};}),meta:{timezone:'Asia/Kolkata'}};
   return {data:[]};
  };
  window.addEventListener('message',event=>{
   if(event.source!==frame.contentWindow||event.data?.jsonrpc!=='2.0')return;
   const m=event.data;
   if(m.method==='ui/initialize')frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result:{protocolVersion:m.params.protocolVersion,hostInfo:{name:'Explorer test host',version:'1'},hostCapabilities:{serverTools:{}},hostContext:{theme:'light',toolInfo:{tool:{name:'portfolio',inputSchema:{type:'object'}}}}}},'*');
   else if(m.method==='ui/notifications/initialized')window.emit('portfolio',{mode:'holdings',from:0,limit:2},pageData(0));
   else if(m.method==='tools/call'){
    window.calls.push(m.params);
    const {name,arguments:args}=m.params;
    const panel=name==='market'&&['quote','historical'].includes(args.mode)||args.type==='summary';
    if((panel&&window.echoPanels)||(!panel&&window.echoMain))window.send('ui/notifications/tool-input',{arguments:args});
    let message;
    if(window.failPage&&name==='portfolio'&&args.from===2){window.failPage=false;message={jsonrpc:'2.0',id:m.id,error:{code:-32000,message:'Temporary bridge failure'}};}
    else if(window.failQuote&&args.mode==='quote'){window.failQuote=false;message={jsonrpc:'2.0',id:m.id,result:{isError:true,content:[{type:'text',text:'Quote temporarily unavailable'}]}};}
    else message={jsonrpc:'2.0',id:m.id,result:window.wrap(name,args,fixture(name,args))};
    const respond=()=>{
     if(((panel&&window.echoPanels)||(!panel&&window.echoMain))&&message.result?._meta)window.send('ui/notifications/tool-result',message.result);
     if(message.result)window.responses.push({name,args,result:message.result});
     frame.contentWindow.postMessage(message,'*');
    };
    if(args.mode==='quote'&&window.deferSymbol===args.instruments[0]){window.deferSymbol=null;window.releaseQuote=respond;}else respond();
   }
  });
  frame.srcdoc=html;
 },html);
 const app=page.frameLocator('iframe');
 await app.getByRole('heading',{name:'Portfolio overview',exact:true}).waitFor();
 assert.match(await app.locator('#account-summary').textContent(),/\+25,000.00/);
 assert.equal(await app.locator('.primary .data-table tbody tr').count(),2);
 assert.equal(await page.evaluate(()=>window.calls.filter(call=>call.arguments.type==='summary').length),1);
 // Summary input and metadata-free result echoes can arrive after its response.
 await page.evaluate(()=>{const {args,result}=window.responses.find(item=>item.args.type==='summary');window.send('ui/notifications/tool-input',{arguments:args});const copy=structuredClone(result);delete copy._meta;window.send('ui/notifications/tool-result',copy);});
 await page.waitForTimeout(50);assert.equal(await app.locator('#title').textContent(),'Holdings');
 assert.equal(await app.getByRole('button',{name:'Refresh',exact:true}).isEnabled(),true);
 await app.locator('#filter').fill('INFY');
 assert.equal(await app.locator('.primary .data-table tbody tr').count(),1);
 assert.match(await app.locator('#account-summary').textContent(),/\+25,000.00/,'account totals must not change with page filters');
 await app.locator('#filter').fill('');
 await page.setViewportSize({width:444,height:904});await capture('explorer-holdings-mobile-light');await page.setViewportSize({width:1180,height:1050});
 // Bridge failure while paging retains the attempted page for Retry.
 await page.evaluate(()=>window.failPage=true);
 await app.getByRole('button',{name:'Next',exact:true}).click();
 await app.getByText('Temporary bridge failure',{exact:true}).waitFor();
 await app.getByRole('button',{name:'Retry',exact:true}).click();
 await app.getByText('Rows 3–3 of 3',{exact:false}).waitFor();
 const pages=await page.evaluate(()=>window.calls.filter(call=>call.name==='portfolio'&&call.arguments.from===2));assert.equal(pages.length,2);
 await app.getByRole('button',{name:'Previous',exact:true}).click();
 await app.locator('.primary .data-table tbody tr').nth(1).waitFor();
 assert.equal(await page.evaluate(()=>window.calls.filter(call=>call.arguments.type==='summary').length),1,'pagination and duplicate result echoes must not refetch the summary');
 await app.getByRole('button',{name:'Refresh',exact:true}).click();
 await app.getByRole('heading',{name:'Portfolio overview',exact:true}).waitFor();
 assert.equal(await page.evaluate(()=>window.calls.filter(call=>call.arguments.type==='summary').length),2,'explicit refresh fetches exactly one new summary');
 // Opening an instrument fetches a quote; echoed panel notifications cannot replace holdings.
 await app.locator('.primary .data-table').getByRole('button',{name:'INFY',exact:true}).click();
 await app.locator('#inspector').getByText('₹1,523.80',{exact:true}).waitFor();
 assert.equal(await app.locator('#title').textContent(),'Holdings');
 assert.equal(await app.locator('.instrument-list .instrument-row:visible').count(),2);
 await page.setViewportSize({width:444,height:904});await capture('explorer-overview-mobile-light');await page.setViewportSize({width:1180,height:1050});
 await app.getByRole('tab',{name:'Depth',exact:true}).click();
 assert.equal(await app.locator('#inspector .depth-side tbody tr').count(),10);
 assert.equal(await app.locator('#inspector meter').count(),0);
 assert.equal(await app.locator('#inspector .buy .depth-bar').evaluateAll(nodes=>new Set(nodes.map(node=>getComputedStyle(node).backgroundColor)).size),1);
 assert.match(await app.locator('#inspector').textContent(),/15,000/);
 await capture('explorer-depth-desktop');
 await app.getByRole('tab',{name:'Chart',exact:true}).click();
 await app.locator('#inspector canvas').waitFor();
 assert.equal(await app.locator('#title').textContent(),'Holdings');
 const chartArgs=await page.evaluate(()=>window.calls.find(call=>call.name==='market'&&call.arguments.mode==='historical').arguments);
 assert.equal(chartArgs.instrument_token,111);assert.equal(chartArgs.interval,'day');
 await app.locator('#inspector canvas').focus();await page.keyboard.press('Home');
 const before=await app.locator('#inspector .chart-readout').textContent();await page.keyboard.press('ArrowRight');
 assert.notEqual(await app.locator('#inspector .chart-readout').textContent(),before,'keyboard users can inspect candles');
 await capture('explorer-chart-desktop');
 await app.locator('#chart-from').fill('2026-10-01');await app.locator('#chart-to').fill('2026-10-20');await app.locator('#chart-interval').selectOption('15minute');
 await app.getByRole('button',{name:'Load chart',exact:true}).click();
 await page.waitForFunction(()=>window.calls.some(call=>call.arguments.mode==='historical'&&call.arguments.interval==='15minute'&&call.arguments.from_date==='2026-10-01'&&call.arguments.to_date==='2026-10-20'));
 await app.locator('#inspector canvas').waitFor();
 // Editing a draft must not relabel the already loaded chart's period.
 await app.locator('#chart-from').fill('2026-09-01');
 await app.getByRole('button',{name:'Refresh quote',exact:true}).click();
 await app.getByRole('button',{name:'Refresh quote',exact:true}).waitFor();
 assert.match(await app.locator('#inspector-body').textContent(),/2026-10-01 to 2026-10-20 · 15minute/);
 assert.doesNotMatch(await app.locator('#inspector-body').textContent(),/2026-09-01 to/);
 // Cross-selection race: an old response cannot revive the previous instrument.
 await app.getByRole('button',{name:'Back to list',exact:true}).click();
 await page.evaluate(()=>window.deferSymbol='NSE:INFY');
 await app.locator('.primary .data-table').getByRole('button',{name:'INFY',exact:true}).click();
 await page.waitForFunction(()=>typeof window.releaseQuote==='function');
 await app.locator('.instrument-list .instrument-row').filter({hasText:'TCS'}).click();
 await app.locator('#inspector').getByText('₹3,540.25',{exact:true}).waitFor();
 await page.evaluate(()=>{window.send('ui/notifications/tool-input',{arguments:window.calls.findLast(call=>call.arguments.mode==='quote'&&call.arguments.instruments[0]==='NSE:INFY').arguments});window.releaseQuote();});
 await page.waitForTimeout(100);
 assert.equal(await app.locator('#inspector h2').textContent(),'TCS');
 assert.equal(await app.locator('#title').textContent(),'Holdings');
 // A failed refresh explicitly marks the old quote, then recovers.
 await page.evaluate(()=>window.failQuote=true);
 await app.getByRole('button',{name:'Refresh quote',exact:true}).click();
 await app.locator('#inspector .panel-error').filter({hasText:'previous snapshot'}).waitFor();
 await app.getByRole('button',{name:'Refresh quote',exact:true}).click();
 await app.getByRole('button',{name:'Refresh quote',exact:true}).waitFor();
 await app.locator('#inspector').getByText('₹3,540.25',{exact:true}).waitFor();
 assert.equal(await app.locator('#inspector .panel-error').count(),0);
 // Mobile selection replaces only the list; Back restores it and focus.
 await page.setViewportSize({width:390,height:1050});
 await page.evaluate(()=>window.send('ui/notifications/host-context-changed',{theme:'dark'}));
 assert.equal(await app.locator('.primary').isVisible(),false);
 await app.getByRole('tab',{name:'Overview',exact:true}).click();
 await capture('explorer-inspector-mobile-dark');
 assert.equal(await app.locator('body').evaluate(node=>node.scrollWidth>window.innerWidth),false);
 await app.getByRole('button',{name:'Back to list',exact:true}).click();
 assert.equal(await app.locator('.primary').isVisible(),true);
 assert.equal(await app.locator('.primary .data-table').isVisible(),false);
 await capture('explorer-holdings-mobile-dark');
 assert.equal(await app.locator('.instrument-row:focus').count(),1,'Back restores focus to the visible mobile row');
 // Product identity remains visible for same-symbol positions.
 await page.evaluate(()=>window.emit('portfolio',{mode:'positions'},{data:[
  {tradingsymbol:'INFY',exchange:'NSE',instrument_token:111,product:'MIS',view:'net',quantity:5,average_price:1500,last_price:1523.8,pnl:119,realised:0,unrealised:119},
  {tradingsymbol:'INFY',exchange:'NSE',instrument_token:111,product:'CNC',view:'net',quantity:2,average_price:1500,last_price:1523.8,pnl:47.6,realised:0,unrealised:47.6},
  {tradingsymbol:'INFY',exchange:'NSE',instrument_token:111,product:'MIS',view:'day',quantity:0,average_price:1500,last_price:1523.8,pnl:100,realised:100,unrealised:0}
 ]}));
 await app.getByRole('heading',{name:'Positions',exact:true}).waitFor();
 assert.equal(await app.locator('.instrument-row:visible').count(),2);
 assert.match(await app.locator('.instrument-row:visible').nth(0).textContent(),/MIS/);assert.match(await app.locator('.instrument-row:visible').nth(1).textContent(),/CNC/);
 await app.locator('#view-options summary').click();
 await app.locator('#position-state').selectOption('closed');
 await app.getByText('No matches on this page',{exact:true}).waitFor();
 await app.locator('#position-state').selectOption('');
 await app.locator('#view').selectOption('day');assert.equal(await app.locator('.instrument-row:visible').count(),1);assert.match(await app.locator('.instrument-row:visible').textContent(),/Closed/);
 await capture('explorer-positions-mobile-dark');
 // Full contracts can be filtered and opened without losing the finder.
 await page.setViewportSize({width:1180,height:1050});
 await page.evaluate(()=>{window.send('ui/notifications/host-context-changed',{theme:'light'});window.emit('market',{mode:'search',query:'NIFTY',verbosity:'full'},{data:window.contracts,pagination:{from:0,limit:100,total:3,returned:3,has_more:false}});});
 await app.getByRole('heading',{name:'Instrument finder',exact:true}).waitFor();
 await app.locator('#contract-type').selectOption('CE');
 assert.equal(await app.locator('.primary .data-table tbody tr').count(),1);
 await app.locator('#contract-expiry').selectOption('2026-11-24');
 await app.getByText('No matches on this page',{exact:true}).waitFor();
 await app.locator('#contract-expiry').selectOption('');
 await app.locator('.primary .data-table').getByRole('button',{name:'NIFTY26OCT25000CE',exact:true}).click();
 await app.locator('#inspector').getByText('₹125.50',{exact:true}).waitFor();
 assert.equal(await app.locator('#title').textContent(),'Instrument finder');
 await capture('explorer-contract-desktop');
 await app.getByRole('button',{name:'Back to list',exact:true}).click();
 await app.locator('#finder-query').fill('NIFTY26NOV');await app.locator('#finder-live').check();await app.getByRole('button',{name:'Search instruments',exact:true}).click();
 await page.waitForFunction(()=>window.calls.some(call=>call.arguments.query==='NIFTY26NOV'));
 const query=await page.evaluate(()=>window.calls.find(call=>call.arguments.query==='NIFTY26NOV').arguments);assert.equal(query.verbosity,'full');assert.equal(query.include_live_quote,true);assert.equal(query.filter_on,'id');
 await app.locator('.primary .data-table tbody tr').waitFor();
 assert.equal(await app.locator('.primary .data-table tbody tr').count(),1,'fixture uses the same substring matching as the server');
 await app.locator('#display').selectOption('list');
 assert.doesNotMatch(await app.locator('.instrument-row:visible').textContent(),/Strike/);
 assert.equal(await app.locator('.instrument-list').evaluate(node=>[...node.children].every(child=>child.getAttribute('role')==='listitem')),true);
 await page.evaluate(()=>window.emit('market',{mode:'search',query:'Infosys',filter_on:'name',verbosity:'full'},{data:window.companies}));
 await app.locator('.instrument-row:visible').waitFor();
 assert.equal(await app.locator('#finder-field').inputValue(),'name','chat company-search mode is retained');
 await app.getByRole('button',{name:'Search instruments',exact:true}).click();
 await page.waitForFunction(()=>window.calls.some(call=>call.arguments.query==='Infosys'&&call.arguments.filter_on==='name'));
 await app.locator('.instrument-row:visible').waitFor();
 assert.match(await app.locator('.instrument-row:visible').textContent(),/INFY/);
 // Unsettled holdings contribute to both quantity and derived values.
 await page.evaluate(()=>window.emit('portfolio',{mode:'holdings'},{data:[{tradingsymbol:'T1TEST',exchange:'NSE',instrument_token:777,quantity:0,t1_quantity:10,average_price:100,last_price:150,pnl:500}]}));
 await app.getByRole('heading',{name:'Holdings',exact:true}).waitFor();
 assert.match(await app.locator('.instrument-row:visible').textContent(),/Qty 10/);
 assert.match(await app.locator('.instrument-row:visible').textContent(),/Includes T1 10/);
 assert.match(await app.locator('.instrument-row:visible').textContent(),/Invested 1,000.00/);
 await app.locator('.instrument-row:visible').click();
 assert.match(await app.locator('#inspector').textContent(),/1,500.00/);
 await app.getByRole('button',{name:'Back to list',exact:true}).click();
 // Profile values are text, not currency, even with numeric metadata mixed in.
 await page.evaluate(()=>window.emit('portfolio',{mode:'profile'},{user_name:'Example User',email:'example@example.com',broker:'ZERODHA'}));
 await app.getByRole('heading',{name:'Account',exact:true}).waitFor();
 assert.equal(await app.getByRole('columnheader',{name:'Value',exact:false}).count(),1);
 assert.doesNotMatch(await app.locator('.primary th').allTextContents().then(items=>items.join(' ')),/₹/);
 assert.equal(await app.locator('.primary td[title="INR"]').count(),0);
 // Funds render the actual segment values, including disabled segments.
 await page.evaluate(()=>{window.send('ui/notifications/tool-input',{arguments:{mode:'margins'}});const copy=structuredClone(window.responses.find(item=>item.args.mode==='quote').result);delete copy._meta;window.send('ui/notifications/tool-result',copy);});
 await page.waitForTimeout(50);
 await app.getByText('Loading snapshot',{exact:true}).waitFor();
 await page.evaluate(()=>window.send('ui/notifications/tool-result',window.wrap('portfolio',{mode:'margins'},{equity:{enabled:true,net:23456.75,available:{cash:30000,collateral:1000},utilised:{debits:7543.25,span:5000}},commodity:{enabled:false,net:0}})));
 await app.getByRole('heading',{name:'Funds & margins',exact:true}).waitFor();
 await app.getByText('23,456.75',{exact:true}).waitFor();assert.equal(await app.locator('.funds-segment').count(),2);
 await capture('explorer-funds-desktop');
 // A later chat request may repeat a panel query and even return the same data.
 await page.evaluate(()=>{
  const previous=window.responses.find(item=>item.args.mode==='quote'&&item.args.instruments[0]==='NSE:INFY');
  const args={mode:'quote',instruments:['NSE:INFY']};window.emit('market',args,previous.result.structuredContent);
 });
 await app.getByRole('heading',{name:'Quotes',exact:true}).waitFor();
 assert.equal(await app.getByRole('button',{name:'Refresh',exact:true}).isEnabled(),true);
 await page.evaluate(()=>window.send('ui/notifications/tool-input',{arguments:{mode:'quote',instruments:['NSE:INFY']}}));
 await app.getByText('Loading snapshot',{exact:true}).waitFor();
 await page.evaluate(()=>{
  const previous=window.responses.find(item=>item.args.mode==='quote'&&item.args.instruments[0]==='NSE:INFY');
  const copy=structuredClone(previous.result);delete copy._meta;window.send('ui/notifications/tool-result',copy);
 });
 await app.locator('.primary .data-views').waitFor();
 assert.equal(await app.getByRole('button',{name:'Refresh',exact:true}).isEnabled(),true);
 await page.evaluate(()=>{
  const previous=window.responses.find(item=>item.args.type==='summary');window.emit('portfolio',{mode:'holdings',type:'summary'},previous.result.structuredContent);
 });
 await app.getByRole('heading',{name:'Portfolio overview',exact:true}).waitFor();
 assert.equal(await app.locator('#title').textContent(),'Holdings');
 // A host without serverTools still renders snapshots and source details.
 const offline=await browser.newPage({viewport:{width:390,height:850}});
 await offline.setContent('<iframe title="Offline host" sandbox="allow-scripts" style="width:100%;height:800px;border:0"></iframe>');
 await offline.evaluate(html=>{
  const frame=document.querySelector('iframe');window.calls=[];
  window.emitSnapshot=(tool,args,data)=>{
   frame.contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/tool-input',params:{arguments:args}},'*');
   frame.contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/tool-result',params:{structuredContent:data,_meta:{'trade.kite/app':{tool,arguments:args}}}},'*');
  };
  window.addEventListener('message',event=>{
   if(event.source!==frame.contentWindow||event.data?.jsonrpc!=='2.0')return;const m=event.data;
   if(m.method==='ui/initialize')frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result:{protocolVersion:m.params.protocolVersion,hostInfo:{name:'Read-only host',version:'1'},hostCapabilities:{},hostContext:{theme:'light',toolInfo:{tool:{name:'market',inputSchema:{type:'object'}}}}}},'*');
   else if(m.method==='ui/notifications/initialized'){
    frame.contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/tool-input',params:{arguments:{mode:'quote',instruments:['NSE:INFY']}}},'*');
    frame.contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/tool-result',params:{structuredContent:{data:{'NSE:INFY':{last_price:1500,ohlc:{open:1490,high:1520,low:1480,close:1495}}}},_meta:{'trade.kite/app':{tool:'market',arguments:{mode:'quote',instruments:['NSE:INFY']}}}}},'*');
   }else if(m.method==='tools/call')window.calls.push(m.params);
  });frame.srcdoc=html;
 },html);
 const offlineApp=offline.frameLocator('iframe');
 await offlineApp.locator('.instrument-row:visible').waitFor();
 assert.equal(await offlineApp.getByRole('button',{name:'Refresh',exact:true}).isDisabled(),true);
 await offlineApp.locator('.instrument-row:visible').click();
 await offlineApp.locator('#inspector').getByText('₹1,500.00',{exact:true}).waitFor();
 assert.equal(await offlineApp.getByRole('button',{name:'Refresh quote',exact:true}).isDisabled(),true);
 assert.deepEqual(await offline.evaluate(()=>window.calls),[]);
 await offlineApp.getByRole('button',{name:'Back to list',exact:true}).click();
 await offline.evaluate(()=>window.emitSnapshot('market',{mode:'ltp',instruments:['CDS:USDINR']},{data:{'CDS:USDINR':{last_price:83.2475,instrument_token:999}}}));
 await offlineApp.getByRole('heading',{name:'Last traded prices',exact:true}).waitFor();
 await offlineApp.locator('.instrument-row:visible').getByText('83.2475',{exact:true}).waitFor();
 await offline.evaluate(()=>window.emitSnapshot('market',{mode:'ohlc',instruments:['CDS:USDINR']},{data:{'CDS:USDINR':{last_price:83.2475,ohlc:{open:83.24,high:83.26,low:83.23,close:83.245}}}}));
 await offlineApp.getByRole('heading',{name:'OHLC',exact:true}).waitFor();
 await offlineApp.locator('.instrument-row:visible').click();
 await offlineApp.locator('#inspector').getByText('₹83.2475',{exact:true}).waitFor();
 await offlineApp.getByRole('button',{name:'Back to list',exact:true}).click();
 await offline.evaluate(()=>window.emitSnapshot('market',{mode:'quote',instruments:['CDS:USDINR']},{data:{'CDS:USDINR':{last_price:83.2475,ohlc:{open:83.24,high:83.26,low:83.23,close:83.245},depth:{buy:[{price:83.2475,orders:1,quantity:10},{price:83.245,orders:1,quantity:20}],sell:[{price:83.25,orders:1,quantity:15}]}}}}));
 await offlineApp.getByRole('heading',{name:'Quotes',exact:true}).waitFor();
 await offlineApp.locator('.instrument-row:visible').click();
 await offlineApp.getByRole('tab',{name:'Depth',exact:true}).click();
 assert.deepEqual(await offlineApp.locator('.buy tbody tr td:first-child').allTextContents(),['83.2475','83.245']);
 await offline.screenshot({path:new URL('../test-artifacts/review-currency-depth-mobile.png',import.meta.url).pathname});
 await offlineApp.getByRole('button',{name:'Back to list',exact:true}).click();
 await offline.evaluate(()=>window.emitSnapshot('portfolio',{mode:'holdings',type:'summary'},{invested_amount:12345678.9,current_value:13345678.9,total_pnl:1000000,total_pnl_percent:8.1,today_pnl:1000,today_pnl_percent:.01}));
 await offlineApp.getByRole('heading',{name:'Portfolio overview',exact:true}).waitFor();
 await offline.setViewportSize({width:700,height:850});
 assert.equal(await offlineApp.locator('body').evaluate(node=>node.scrollWidth>window.innerWidth),false);
 assert.equal(await offlineApp.locator('.account-metrics strong').evaluateAll(nodes=>nodes.every(node=>node.getBoundingClientRect().height<40&&node.scrollWidth<=node.clientWidth)),true,'large totals stay on one line at tablet widths');
 await offline.screenshot({path:new URL('../test-artifacts/review-summary-tablet.png',import.meta.url).pathname});
 assert.deepEqual(await offline.evaluate(()=>window.calls),[]);
 await offline.close();
 assert.deepEqual(network,[],'all data must use the host tool bridge, never direct network');assert.deepEqual(failures,[]);
 console.log('Explorer checks passed: independent account summary, retry page identity, linked inspector, five-level depth, chart keyboard navigation, echoed notifications, stale response isolation, quote recovery, mobile navigation, product identity, contract filters, server search, and funds cards.');
}finally{await browser.close();}
