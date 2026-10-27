import { chromium } from 'playwright';
import { readFile, mkdir } from 'node:fs/promises';
import assert from 'node:assert/strict';
const html=await readFile(new URL('../app.html',import.meta.url),'utf8');
const browser=await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH?{executablePath:process.env.CHROMIUM_PATH}:{}),args:['--no-sandbox']});
const page=await browser.newPage({viewport:{width:1180,height:1100}}),errors=[],network=[];
page.on('pageerror',error=>errors.push(error.message));page.on('request',request=>{if(/^https?:/.test(request.url()))network.push(request.url());});
await mkdir(new URL('../test-artifacts/',import.meta.url),{recursive:true});
const capture=async name=>{await page.evaluate(()=>window.scrollTo(0,0));await page.frameLocator('iframe').locator('body').evaluate(()=>window.scrollTo(0,0));return page.screenshot({path:new URL(`../test-artifacts/widget-${name}.png`,import.meta.url).pathname});};
try{
 await page.setContent('<iframe title="Kite" sandbox="allow-scripts" style="width:100%;height:1050px;border:0"></iframe>');
 await page.evaluate(html=>{
  const frame=document.querySelector('iframe');window.calls=[];
  const holdings=[{exchange:'NSE',tradingsymbol:'INFY',quantity:25,average_price:1400,last_price:1600,pnl:5000},{exchange:'NSE',tradingsymbol:'TCS',quantity:10,average_price:3500,last_price:3400,pnl:-1000},{exchange:'NSE',tradingsymbol:'HDFCBANK',quantity:20,average_price:1700,last_price:1800,pnl:2000}];
  const positions=[{exchange:'NSE',tradingsymbol:'INFY',quantity:10,product:'MIS',average_price:1500,last_price:1600,pnl:1000,view:'net'},{exchange:'NSE',tradingsymbol:'TCS',quantity:-5,product:'MIS',average_price:3300,last_price:3400,pnl:-500,view:'net'},{exchange:'NSE',tradingsymbol:'INFY',quantity:20,product:'MIS',average_price:1500,last_price:1600,pnl:2000,view:'day'}];
  const contracts=[25000,25100].flatMap((strike,index)=>['CE','PE'].map((instrument_type,i)=>({id:`NFO:NIFTY26OCT${strike}${instrument_type}`,exchange:'NFO',tradingsymbol:`NIFTY26OCT${strike}${instrument_type}`,instrument_type,strike,expiry_date:'2026-10-27',instrument_token:444+index*2+i})));
  const paged=(data,args)=>({data:data.slice(args.from||0,(args.from||0)+(args.limit||100)),pagination:{from:args.from||0,returned:data.length,limit:args.limit||100,total:data.length,has_more:false}});
  const fixture=(name,args)=>{
   if(name==='portfolio'){
    if(args.mode==='margins')return {equity:{enabled:true,net:245000,available:{cash:200000,collateral:45000},utilised:{debits:12500}},commodity:{enabled:false}};
    if(args.type==='summary')return {invested_amount:104000,current_value:110000,total_pnl:6000,total_pnl_percent:5.77,today_pnl:500};
    return paged(args.mode==='positions'?positions:holdings,args);
   }
   if(name==='orders_read'){
    if(args.mode==='history')return {data:[{status:'OPEN',order_timestamp:'2026-10-27T09:20:00+05:30'},{status:'COMPLETE',order_timestamp:'2026-10-27T09:21:00+05:30'}]};
    if(args.mode==='trades')return {data:[{quantity:10,average_price:1600,fill_timestamp:'2026-10-27T09:21:00+05:30'}]};
    return paged([{order_id:'123',tradingsymbol:'INFY',exchange:'NSE',product:'CNC',transaction_type:'BUY',order_type:'LIMIT',status:'COMPLETE',quantity:10,filled_quantity:10,price:1600,average_price:1600},{order_id:'124',tradingsymbol:'TCS',exchange:'NSE',transaction_type:'BUY',status:'REJECTED',quantity:5,filled_quantity:0,price:3400,status_message:'Insufficient funds'}],args);
   }
   if(name==='gtt_read')return paged([{id:42,status:'active',type:'two-leg',condition:{exchange:'NSE',tradingsymbol:'INFY',trigger_values:[1500,1700]},orders:[{transaction_type:'SELL',quantity:10,price:1495,product:'CNC'},{transaction_type:'SELL',quantity:10,price:1700,product:'CNC'}]}],args);
   if(args.mode==='option_chain'){
    const source=window.largeChain?Array.from({length:601},(_,i)=>{const strike=10000+i*50;return {strike,...Object.fromEntries(['call','put'].map((side,j)=>{const instrument_type=j?'PE':'CE';return [side,{id:`NFO:NIFTY26OCT${strike}${instrument_type}`,exchange:'NFO',tradingsymbol:`NIFTY26OCT${strike}${instrument_type}`,instrument_type,strike,expiry_date:'2026-10-27',instrument_token:1000+i*2+j}];}))};}):null;
    const strikes=source||[25000,25100].map(strike=>({strike,call:contracts.find(c=>c.strike===strike&&c.instrument_type==='CE'),put:contracts.find(c=>c.strike===strike&&c.instrument_type==='PE')}));
    return {data:{exchange:'NFO',underlying_exchange:'NSE',underlying_name:'NIFTY 50',underlying:{id:'NSE:NIFTY 50',exchange:'NSE',tradingsymbol:'NIFTY 50'},future:null,expiry:'2026-10-27',expiry_type:'weekly',expiries:['2026-10-27'],strikes:strikes.slice(args.from||0,(args.from||0)+(args.limit||500)),loaded_at:'2026-10-27T08:00:00+05:30',active_policy:'active_derivatives_only'},pagination:{from:args.from||0,limit:args.limit||500,total:strikes.length,returned:Math.min(args.limit||500,Math.max(0,strikes.length-(args.from||0))),has_more:(args.from||0)+(args.limit||500)<strikes.length},meta:{catalog_revision:'2026-10-27T08:00:00+05:30',pagination_unit:'strikes',complete:true}};
   }
   if(args.mode==='search')return paged(contracts,args);
   if(args.mode==='quote')return {data:Object.fromEntries(args.instruments.map((id,index)=>{
    const strike=Number(id.match(/OCT(\d+)/)?.[1]),distance=Math.abs(strike-25050);
    const price=window.largeChain?(id.startsWith('NSE:')?25050:Math.round((Math.max(0,id.endsWith('CE')?25050-strike:strike-25050)+Math.max(.05,125-distance*.02))*20)/20):id.startsWith('NSE:')?1600:125-index*10;
    return [id,{last_price:price,oi:window.largeChain?Math.round(560000/(1+distance/1000)):560000+index*20000,volume:window.largeChain?Math.round(120000/(1+distance/500)):120000,depth:{buy:[{price:Math.max(.05,Math.round((price-.05)*20)/20)}],sell:[{price:Math.round((price+.05)*20)/20}]},timestamp:'2026-10-27T13:45:00+05:30'}];
   }))};
   return {data:[]};
  };
  const send=(method,params)=>frame.contentWindow.postMessage({jsonrpc:'2.0',method,params},'*');
  window.emit=(name,args)=>{send('ui/notifications/tool-input',{arguments:args});send('ui/notifications/tool-result',wrap(name,args));};
  window.launchView=(view,extra={})=>{
   const original={view,...extra};
   const [name,args]=view==='orders'?['orders_read',{mode:'list',from:0,limit:100}]:view==='gtt'?['gtt_read',{mode:'list',from:0,limit:100}]:view==='option_chain'?['market',{mode:'option_chain',underlying:extra.underlying||'NIFTY',from:0,limit:500}]:['portfolio',{mode:view==='funds'?'margins':view,...(view==='holdings'?{type:'full'}:{}),from:0,limit:100}];
   if(extra.from!==undefined)args.from=extra.from;
   const result=wrap(name,args);result._meta['trade.kite/app'].origin={tool:'open_kite_view',arguments:original};
   send('ui/notifications/tool-input',{arguments:original});send('ui/notifications/tool-result',result);
  };
  const wrap=(name,args)=>({structuredContent:fixture(name,args),_meta:{'trade.kite/app':{tool:name,arguments:args}}});
  window.addEventListener('message',event=>{
   if(event.source!==frame.contentWindow||event.data?.jsonrpc!=='2.0')return;const m=event.data;
   if(m.method==='ui/initialize')frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result:{protocolVersion:m.params.protocolVersion,hostInfo:{name:'Widget fixture',version:'1'},hostCapabilities:{serverTools:{}},hostContext:{theme:'light',toolInfo:{tool:{name:'portfolio',inputSchema:{type:'object'}}}}}},'*');
   else if(m.method==='ui/notifications/initialized'){const args={mode:'holdings',type:'full',from:0,limit:100};send('ui/notifications/tool-input',{arguments:args});send('ui/notifications/tool-result',wrap('portfolio',args));}
   else if(m.method==='tools/call'){
    window.calls.push(m.params);const {name,arguments:args}=m.params,result=wrap(name,args);
    const respond=()=>{send('ui/notifications/tool-input',{arguments:args});send('ui/notifications/tool-result',result);frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result},'*');};
    if(window.delayChain&&args.mode==='option_chain'){window.releaseChain=respond;}else respond();
   }else if(m.id)frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result:{}},'*');
  });frame.srcdoc=html;
 },html);
 const app=page.frameLocator('iframe');
 await app.getByRole('heading',{name:'Portfolio overview',exact:true}).waitFor();
 await app.getByText('Holdings allocation',{exact:true}).click();
 assert.equal(await app.locator('.treemap-tile').count(),3);await capture('holdings-treemap');
 await app.getByLabel('Holdings allocation basis').selectOption('pnl');assert.equal(await app.locator('.treemap-tile').count(),3);
 await app.locator('#widget-nav').getByRole('button',{name:'Positions',exact:true}).click();
 await app.getByText('Position P&L chart',{exact:true}).click();assert.equal(await app.locator('.pnl-row').count(),2);await capture('positions-chart');
 await app.locator('#view').selectOption('day');await app.getByText('Position P&L chart',{exact:true}).click();assert.equal(await app.locator('.pnl-row').count(),1);
 await app.locator('#position-state').selectOption('closed');
 await app.locator('#widget-nav').getByRole('button',{name:'Holdings',exact:true}).click();
 await app.getByRole('heading',{name:'Holdings',exact:true}).waitFor();
 assert.equal(await app.locator('.treemap-tile').count(),3,'position filters must not hide holdings');
 assert.equal(await app.getByLabel('Holdings allocation basis').inputValue(),'pnl','chart basis survives view changes');
 assert.equal(await app.locator('.portfolio-chart').evaluate(node=>node.open),true,'chart expansion survives view changes');
 await app.locator('#widget-nav').getByRole('button',{name:'Orders',exact:true}).click();
 await app.getByRole('heading',{name:'Order book',exact:true}).waitFor();assert.equal(await app.locator('.order-card').count(),2);
 await app.getByRole('button',{name:'Timeline',exact:true}).first().click();await app.getByRole('heading',{name:'Order timeline',exact:true}).waitFor();assert.equal(await app.locator('.order-timeline li').count(),2);await capture('order-timeline');
 await app.getByRole('button',{name:'Fills',exact:true}).first().click();await app.getByRole('heading',{name:'Order fills',exact:true}).waitFor();assert.match(await app.locator('.order-detail').first().textContent(),/1,600/);
 await app.locator('#widget-nav').getByRole('button',{name:'GTT',exact:true}).click();await app.getByRole('heading',{name:'GTT monitor',exact:true}).waitFor();
 await app.getByRole('button',{name:'Refresh trigger prices',exact:true}).click();await app.getByText('Latest ₹1,600.00',{exact:false}).waitFor();assert.match(await app.locator('.gtt-card').textContent(),/-6.25% from quote/);assert.match(await app.locator('.gtt-card').textContent(),/\+6.25% from quote/);await capture('gtt-monitor');
 await app.locator('#widget-nav').getByRole('button',{name:'Option chain',exact:true}).click();await app.getByRole('button',{name:'Load chain',exact:true}).click();
 await app.getByText('4/4 contracts.',{exact:false}).waitFor();assert.equal(await app.locator('.option-chain tbody tr').count(),2);await capture('option-chain');
 await app.getByRole('button',{name:'Refresh',exact:true}).click();await app.getByText('4/4 contracts.',{exact:false}).waitFor();
 await page.setViewportSize({width:390,height:1000});await capture('option-chain-mobile');
 assert.equal(await app.locator('.option-chain thead tr:last-child th:visible').count(),3);
 await app.getByRole('button',{name:'Show quote details',exact:true}).click();
 assert.equal(await app.locator('.option-chain thead tr:last-child th:visible').count(),11);
 await app.getByRole('button',{name:'Refresh quotes',exact:true}).click();
 await app.getByText('4/4 contracts.',{exact:false}).waitFor();
 assert.equal(await app.getByRole('button',{name:'Hide quote details',exact:true}).getAttribute('aria-pressed'),'true');
 await app.getByRole('button',{name:'Hide quote details',exact:true}).click();
 assert.equal(await app.locator('body').evaluate(node=>node.scrollWidth>window.innerWidth),false);
 await app.locator('#widget-nav').getByRole('button',{name:'GTT',exact:true}).click();await app.getByRole('heading',{name:'GTT monitor',exact:true}).waitFor();await capture('gtt-mobile');
 await page.evaluate(()=>window.emit('market',{mode:'option_chain',underlying:'NIFTY',exchange:'NSE',from:0,limit:500}));
 await app.getByText('4/4 contracts.',{exact:false}).waitFor();
 assert.equal(await app.locator('#options-view').isVisible(),true,'model-originated option-chain result opens the chain view');
 assert.equal(await app.locator('.option-chain tbody tr').count(),2);
 // A model-visible launcher must open each view from its original arguments.
 for(const [view,selector] of [['holdings','#snapshot-view'],['positions','#snapshot-view'],['funds','#snapshot-view'],['orders','#orders-view'],['gtt','#gtt-view'],['option_chain','#options-view']]){
  await page.evaluate(view=>window.launchView(view),view);
  await app.locator(selector).waitFor({state:'visible'});
  if(view==='option_chain')await app.getByText('4/4 contracts.',{exact:false}).waitFor();
  else if(view==='orders')await app.getByRole('heading',{name:'Order book',exact:true}).waitFor();
  else if(view==='gtt')await app.getByRole('heading',{name:'GTT monitor',exact:true}).waitFor();
  else await app.getByRole('heading',{name:view==='funds'?'Funds & margins':view==='holdings'?'Holdings':'Positions',exact:true}).waitFor();
 }
 // Seeded later pages must survive either refresh control.
 for(const [view,heading,label,toolName] of [['orders','Order book','Refresh orders','orders_read'],['gtt','GTT monitor','Refresh GTTs','gtt_read']]){
  await page.evaluate(view=>window.launchView(view,{from:100}),view);
  await app.getByRole('heading',{name:heading,exact:true}).waitFor();
  for(const refresh of ['Refresh',label]){
   await app.getByRole('button',{name:refresh,exact:true}).click();
   await app.getByRole('heading',{name:heading,exact:true}).waitFor();
   assert.equal(await page.evaluate(name=>window.calls.findLast(call=>call.name===name)?.arguments.from,toolName),100,'refresh retains model-selected page');
  }
 }
 // Review every account widget at mobile width in dark mode.
 await page.setViewportSize({width:390,height:1100});
 await page.evaluate(()=>document.querySelector('iframe').contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/host-context-changed',params:{theme:'dark'}},'*'));
 await app.locator('html[data-theme="dark"]').waitFor();
 for(const [name,heading] of [['Holdings','Portfolio overview'],['Positions','Positions'],['Funds','Funds & margins'],['Orders','Order book'],['GTT','GTT monitor']]){
  await app.locator('#widget-nav').getByRole('button',{name,exact:true}).click();
  await app.getByRole('heading',{name:heading,exact:true}).waitFor();
  assert.equal(await app.locator('body').evaluate(node=>node.scrollWidth>window.innerWidth),false,`${name} should fit mobile width`);
  await capture(`review-${name.toLowerCase()}-mobile-dark`);
 }
 await page.evaluate(()=>document.querySelector('iframe').contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/host-context-changed',params:{theme:'light'}},'*'));
 await app.locator('html[data-theme="light"]').waitFor();
 // Large-chain fixture exercises multiple catalog pages and quote batches.
 await page.setViewportSize({width:1180,height:1100});
 await page.evaluate(()=>{window.largeChain=true;window.calls=[];window.emit('market',{mode:'option_chain',underlying:'NIFTY',exchange:'NFO',from:0,limit:500});});
 await app.getByText('1202/1202 contracts.',{exact:false}).waitFor();
 assert.equal(await app.locator('.option-chain tbody tr').count(),601);
 const largeCalls=await page.evaluate(()=>window.calls);
 assert.deepEqual(largeCalls.filter(c=>c.arguments.mode==='option_chain').map(c=>c.arguments.from),[500]);
 assert.deepEqual(largeCalls.filter(c=>c.arguments.mode==='quote').map(c=>c.arguments.instruments.length),[500,500,203]);
 assert.equal(await app.locator('.chain-nearest').getAttribute('data-strike'),'25050');
 const headerGeometry=await app.locator('.option-chain thead').evaluate(node=>{const rows=node.rows;return {firstBottom:rows[0].getBoundingClientRect().bottom,secondTop:rows[1].getBoundingClientRect().top};});
 assert.ok(headerGeometry.secondTop>=headerGeometry.firstBottom,'sticky header rows must not overlap');
 await capture('large-chain-desktop');
 await app.locator('.option-chain .scroll').evaluate(node=>node.scrollTop=node.scrollHeight);
 await capture('large-chain-bottom');
 assert.equal(await app.locator('.option-chain .scroll').evaluate(node=>node.querySelector('tbody tr:last-child').getBoundingClientRect().bottom<=node.getBoundingClientRect().bottom+1),true,'final strike must be reachable');
 await page.setViewportSize({width:390,height:1400});
 await app.getByRole('button',{name:'Near underlying',exact:true}).click();
 assert.equal(await app.locator('.option-chain .scroll').evaluate(node=>{const row=node.querySelector('.chain-nearest').getBoundingClientRect(),box=node.getBoundingClientRect();return row.top>box.top&&row.bottom<box.bottom;}),true);
 await capture('large-chain-mobile');
 assert.equal(await app.locator('body').evaluate(node=>node.scrollWidth>window.innerWidth),false);
 await app.getByRole('button',{name:'Show quote details',exact:true}).click();
 await capture('large-chain-mobile-details');
 await page.evaluate(()=>document.querySelector('iframe').contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/host-context-changed',params:{theme:'dark'}},'*'));
 await app.locator('html[data-theme="dark"]').waitFor();
 await capture('large-chain-mobile-dark');
 await page.setViewportSize({width:1180,height:1100});await capture('large-chain-desktop-dark');
 await page.evaluate(()=>document.querySelector('iframe').contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/host-context-changed',params:{theme:'dark',styles:{variables:{'--color-background-primary':'#202122','--color-text-primary':'#ededed','--font-sans':'Arial, sans-serif','--color-text-success':'#52b788'}}}},'*'));
 await app.locator('html[style*="#202122"]').waitFor();
 assert.match(await app.locator('body').evaluate(node=>getComputedStyle(node).fontFamily),/Arial/);
 assert.equal(await app.locator('body').evaluate(node=>{const probe=document.createElement('span');probe.className='positive';node.append(probe);const color=getComputedStyle(probe).color;probe.remove();return color;}),'rgb(82, 183, 136)','host success color must override dark Kite fallback');
 await app.locator('html').evaluate(node=>{if(getComputedStyle(node).getPropertyValue('--color-background-primary').trim()!=='#202122')throw new Error('Host palette must override Kite fallbacks');});
 // Cancelling an in-flight widget must leave Refresh able to recover.
 await page.evaluate(()=>window.delayChain=true);
 await app.getByRole('button',{name:'Load chain',exact:true}).click();
 await app.getByText('Loading option chain…',{exact:true}).waitFor();
 await page.evaluate(()=>{window.delayChain=false;document.querySelector('iframe').contentWindow.postMessage({jsonrpc:'2.0',method:'ui/notifications/tool-cancelled',params:{}},'*');});
 await app.getByText('Request cancelled',{exact:true}).waitFor();
 assert.equal(await app.locator('#options-view').isVisible(),false);
 await app.getByRole('button',{name:'Refresh',exact:true}).click();
 await app.getByText('1202/1202 contracts.',{exact:false}).waitFor();
 await page.evaluate(()=>window.releaseChain?.());
 assert.equal(await app.locator('.option-chain tbody tr').count(),601,'late cancelled response must not replace the recovered chain');
 const calls=await page.evaluate(()=>window.calls);assert.equal(calls.some(call=>call.arguments.mode==='search'),false,'option chains must use indexed backend discovery');assert.equal(calls.some(call=>['place','modify','cancel','delete'].includes(call.arguments.mode)),false);assert.deepEqual(network,[]);assert.deepEqual(errors,[]);
 console.log('Widget browser checks passed: charts, position views, navigation, orders timeline/fills, GTT quotes/distances, option chain, mobile overflow, bridge-only reads.');
}finally{await browser.close();}
