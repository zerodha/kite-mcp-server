import { App, applyHostStyleVariables } from '@modelcontextprotocol/ext-apps';
import { allowedTools, requestKey, readResult, rowsFor, columnsFor, visibleRows, pageArguments, formatCell, columnLabel, isMoney, isSigned, instrumentRef, rowIdentity, expiryFor, finite, tone, receivedTime, optionStrike } from './model.js';
import { el, action, detailList } from './dom.js';
import { PanelRequests } from './requests.js';
import { Inspector } from './inspector.js';
import { PortfolioSummary, instrumentList, fundsView } from './portfolio.js';
import { priceChart } from './chart.js';

const $ = id => document.getElementById(id);
const compactControls=window.matchMedia('(max-width:600px)');
$('view-options').open=!compactControls.matches;
let disclosureChosen=false;
$('view-options').querySelector('summary').addEventListener('click',()=>{disclosureChosen=true;});
compactControls.addEventListener('change',()=>{if(!disclosureChosen)$('view-options').open=!compactControls.matches;});
const app = new App({ name:'Kite', version:'1.1.0' }, {}, { autoResize:true });
let tool='', args={}, payload=null, rows=[], connected=false, hasResult=false;
let sortKey='', direction=1, inFlight=false, waiting=false, cancelled=false, errorMessage='';
let acceptedRevision=-1, refreshSummaryForRequest=true, countTimer;
let priceBusy=false, priceGeneration=0, priceMessage='';
let revision=0, expectedArgs=null, pendingCall=null, expandedColumns=false, selected=null, chartNode=null, chartRows=null;
const modes={holdings:'Holdings',positions:'Positions',margins:'Funds & margins',profile:'Account',historical:'Historical candles',quote:'Quotes',ltp:'Last traded prices',ohlc:'OHLC',search:'Instrument finder'};
const panelRequests=new PanelRequests((...params)=>app.callServerTool(...params),()=>connected&&hasResult&&!waiting&&!cancelled&&Boolean(app.getHostCapabilities()?.serverTools));
const summary=new PortfolioSummary($('account-summary'),panelRequests);
const inspector=new Inspector($('inspector'),panelRequests,key=>{
  selected=key;
  $('workspace').classList.toggle('has-selection',Boolean(key));
  document.body.classList.toggle('inspecting',Boolean(key));
  for(const button of $('content').querySelectorAll('[data-row-key]'))button.setAttribute('aria-expanded',String(Boolean(key)&&button.dataset.rowKey===key));
});
function status(text,error=false){$('status').textContent=text;$('status').classList.toggle('error',error);}
function canCall(){return connected&&hasResult&&allowedTools.has(tool)&&Boolean(app.getHostCapabilities()?.serverTools);}
function setBusy(){
  const busy=waiting||inFlight;
  $('content').setAttribute('aria-busy',String(busy));
  $('refresh').disabled=busy||!canCall();
  $('refresh-prices').disabled=busy||priceBusy||!canCall();
  $('refresh-prices').textContent=priceBusy?'Refreshing prices…':'Refresh prices';
  const page=payload?.pagination;
  $('previous').disabled=busy||!canCall()||!page||page.from<=0;
  $('next').disabled=busy||!canCall()||!page?.has_more;
  for(const button of $('content').querySelectorAll('[data-retry]'))button.disabled=busy||!canCall();
  $('search-submit').disabled=busy||!canCall();
  $('full-contracts').disabled=busy||!canCall();
}
function hostContext(ctx){
  if(ctx?.styles?.variables)applyHostStyleVariables(ctx.styles.variables);
  if(ctx?.theme){
    document.documentElement.style.colorScheme=ctx.theme;document.documentElement.dataset.theme=ctx.theme;
    const dark=ctx.theme==='dark';
    const defaults={'--color-text-primary':dark?'#e2e5ea':'#252a34','--color-text-secondary':dark?'#a4aebf':'#657083','--color-background-primary':dark?'#181b20':'#fff','--color-background-secondary':dark?'#222730':'#f6f7f9','--color-border-primary':dark?'#657083':'#ccd1d9','--color-border-secondary':dark?'#343b47':'#e8ebef'};
    for(const [key,value] of Object.entries(defaults))if(!ctx.styles?.variables?.[key])document.documentElement.style.setProperty(key,value);
  }
  const name=ctx?.toolInfo?.tool?.name;
  if(!tool&&allowedTools.has(name))tool=name;
}
function resetPanels(keepSummary=false){
  ++priceGeneration;priceBusy=false;priceMessage='';panelRequests.cancel('prices');
  inspector.clear();if(!keepSummary||!summary.data)summary.clear();panelRequests.reset();chartNode?.dispose();chartNode=null;chartRows=null;
}
function statePanel(title,description,retry=false){
  const panel=el('div',null,'empty');panel.append(el('strong',title),el('span',description));
  if(retry&&connected&&app.getHostCapabilities()?.serverTools){const button=action('Retry',()=>call({...args},{refreshSummary:refreshSummaryForRequest}));button.dataset.retry='true';panel.append(button);}
  return panel;
}
function openInstrument(row,opener){inspector.open(row,args.mode,opener);}
function filteredRows(){
  return visibleRows(rows,$('filter').value,$('view').value,sortKey,direction).filter(row=>
    (!$('contract-exchange').value||row.exchange===$('contract-exchange').value)&&
    (!$('contract-type').value||row.instrument_type===$('contract-type').value)&&
    (!$('contract-expiry').value||expiryFor(row)===$('contract-expiry').value)&&
    (!$('position-state').value||($('position-state').value==='open'?row.quantity!==0:$('position-state').value==='closed'?row.quantity===0:$('position-state').value==='long'?row.quantity>0:row.quantity<0)));
}
function fillOptions(id,values,label){
  const select=$(id),previous=select.value;select.replaceChildren();const all=el('option',label);all.value='';select.append(all);
  for(const value of [...new Set(values.filter(Boolean))].sort()){const option=el('option',value);option.value=value;select.append(option);}
  if([...select.options].some(option=>option.value===previous))select.value=previous;
}
function populateFilters(){
  fillOptions('contract-exchange',rows.map(row=>row.exchange),'All exchanges');
  fillOptions('contract-type',rows.map(row=>row.instrument_type),'All types');
  fillOptions('contract-expiry',rows.map(expiryFor),'All expiries');
  const current=$('list-sort').value;
  fillOptions('list-sort',columnsFor(rows).filter(key=>rows.some(row=>typeof row[key]!=='object')),'Original order');
  for(const option of $('list-sort').options)if(option.value)option.textContent=columnLabel(option.value);
  $('list-sort').value=sortKey||current;
}
function renderTable(visible,container){
  const all=columnsFor(rows);
  const defaults=args.mode==='historical'?['date','timestamp','open','high','low','close','volume','oi']:args.mode==='search'?['tradingsymbol','exchange','instrument_type','expiry_date','expiry','strike','lot_size','last_price','latest_price','latest_quote_timestamp']:['instrument','tradingsymbol','exchange','product','quantity','t1_quantity','average_price','last_price','latest_price','latest_quote_timestamp','pnl','net_change','field','value'];
  const primary=all.filter(key=>defaults.includes(key));
  const columns=expandedColumns||!primary.length?all.filter(key=>key!=='view'):primary;
  $('columns').hidden=all.filter(key=>key!=='view').length<=primary.length;
  $('columns').textContent=expandedColumns?'Fewer columns':'More columns';$('columns').setAttribute('aria-pressed',String(expandedColumns));
  const fieldValueRows=all.includes('field')&&all.includes('value');
  const monetary=key=>isMoney(key)&&!(key==='value'&&fieldValueRows);
  const label=key=>args.mode==='holdings'&&key==='quantity'?'Settled qty':columnLabel(key);
  const numeric=new Set(columns.filter(key=>rows.some(row=>typeof row[key]==='number')));
  const table=el('table');table.append(el('caption',`${$('title').textContent} · ${visible.length} rows on this page. Monetary values in INR.`,'sr-only'));
  const head=el('thead'),tr=el('tr');
  for(const key of columns){
    const th=el('th');th.scope='col';if(numeric.has(key))th.className='numeric';
    th.setAttribute('aria-sort',sortKey===key?direction===1?'ascending':'descending':'none');
    const button=action(label(key)+(monetary(key)?' (₹)':''),()=>{direction=sortKey===key?-direction:1;sortKey=key;$('list-sort').value=key;render();});
    button.dataset.sortKey=key;button.title=`Sort this page by ${columnLabel(key)}`;
    const icon=el('span',sortKey===key?direction===1?'↑':'↓':'↕','sort-icon');icon.setAttribute('aria-hidden','true');button.append(icon);th.append(button);tr.append(th);
  }
  head.append(tr);table.append(head);const body=el('tbody');
  for(const row of visible){
    const tr=el('tr');
    for(const key of columns){
      const value=row[key],td=el('td');if(numeric.has(key))td.classList.add('numeric');
      if(finite(value)&&isSigned(key))td.classList.add(tone(value)||'neutral');
      if(monetary(key))td.title='INR';
      if(['tradingsymbol','instrument'].includes(key)&&instrumentRef(row)){
        const button=action(formatCell(value,key),event=>openInstrument(row,event.currentTarget),'symbol-button');button.dataset.rowKey=rowIdentity(row);button.setAttribute('aria-controls','inspector');button.setAttribute('aria-expanded',String(selected===rowIdentity(row)));td.append(button);
      }else if(value&&typeof value==='object'){
        const details=el('details');details.append(el('summary',Array.isArray(value)?`${value.length} items`:`${Object.keys(value).length} fields`),detailList(value));td.append(details);
      }else td.textContent=key==='strike'&&!optionStrike(row)?'—':formatCell(value,key==='value'&&row.field?row.field:key);
      tr.append(td);
    }
    body.append(tr);
  }
  table.append(body);const scroll=el('div',null,'scroll data-table');scroll.tabIndex=0;scroll.setAttribute('role','region');scroll.setAttribute('aria-label','Scrollable snapshot table');scroll.append(table);container.append(scroll);
}
function render(){
  $('price-refresh').hidden=!['holdings','positions','search'].includes(args.mode)||!rows.some(instrumentRef)||waiting||Boolean(errorMessage)||cancelled;
  $('price-status').hidden=$('price-refresh').hidden||!priceMessage;
  $('price-status').textContent=priceMessage;
  $('title').textContent=modes[args.mode]||(args.mode?columnLabel(args.mode):'Kite');
  const previousScroll=$('content').querySelector('.data-table');const scrollLeft=previousScroll?.scrollLeft||0,scrollTop=previousScroll?.scrollTop||0;
  const focusedKey=document.activeElement?.dataset?.sortKey;
  $('content').replaceChildren();
  const hasViews=rows.some(row=>row.view==='day'||row.view==='net');const instrumentRows=rows.some(row=>instrumentRef(row));
  $('controls').dataset.display=instrumentRows?$('display').value:'table';
  const summaryOnly=tool==='portfolio'&&args.mode==='holdings'&&args.type==='summary';
  $('controls').hidden=!rows.length||summaryOnly||args.mode==='margins';$('view-label').hidden=!hasViews;$('position-state-label').hidden=args.mode!=='positions';
  $('display-label').hidden=!instrumentRows;$('list-sort-label').hidden=!instrumentRows;$('sort-direction').hidden=!instrumentRows;
  $('columns').hidden=true;$('contract-filters').hidden=args.mode!=='search'||!rows.length;
  $('finder-form').hidden=args.mode!=='search';$('full-contracts').hidden=args.mode!=='search'||args.verbosity==='full';
  const visible=filteredRows();
  clearTimeout(countTimer);
  const countText=rows.length&&!summaryOnly&&args.mode!=='margins'?`${visible.length} of ${rows.filter(row=>!row.view||row.view===$('view').value).length} rows shown on this page`:'';
  if(!countText)$('result-count').textContent='';else countTimer=setTimeout(()=>{$('result-count').textContent=countText;},300);
  $('sort-direction').textContent=direction===1?'Ascending':'Descending';
  if(waiting||(!hasResult&&!errorMessage)){
    const panel=statePanel('Loading snapshot','Waiting for data from Kite…');for(let i=0;i<3;i++)panel.append(el('div',null,'skeleton'));$('content').append(panel);
  }else if(errorMessage||cancelled)$('content').append(statePanel(cancelled?'Request cancelled':'Could not load snapshot',cancelled?'Refresh to try this request again.':errorMessage,true));
  else if(summaryOnly)$('content').append(el('p','Account-wide holdings summary. Ask for full holdings to inspect individual instruments.','muted'));
  else if(args.mode==='margins'&&fundsView(payload))$('content').append(fundsView(payload));
  else {
    if(args.mode==='historical'){
      if(chartRows!==rows){chartNode?.dispose();chartNode=priceChart(rows);chartRows=rows;}
      $('content').append(chartNode);
    }
    if(visible.length&&visible.every(row=>finite(row.pnl))){
      const total=visible.reduce((sum,row)=>sum+row.pnl,0),block=el('div',null,'summary');block.append(el('span',`P&L · shown ${hasViews?$('view').value+' ':''}rows (INR)`,'muted'),el('strong',formatCell(total,'pnl'),tone(total)));$('content').append(block);
    }
    if(!visible.length)$('content').append(statePanel(rows.length?'No matches on this page':`No ${['holdings','positions'].includes(args.mode)?args.mode:'data'} to display`,rows.length?'Try another search or clear the filters.':'This request returned no rows.'));
    else{
      const group=el('div',null,`data-views ${instrumentRows?'has-instruments':''}`);group.dataset.display=instrumentRows?$('display').value:'table';
      if(instrumentRows)group.append(instrumentList(visible,args.mode,openInstrument,selected));
      renderTable(visible,group);$('content').append(group);
      const scroll=group.querySelector('.data-table');scroll.scrollLeft=scrollLeft;scroll.scrollTop=scrollTop;
      if(focusedKey)[...group.querySelectorAll('[data-sort-key]')].find(button=>button.dataset.sortKey===focusedKey)?.focus({preventScroll:true});
    }
  }
  const page=payload?.pagination;$('pagination').hidden=!page||waiting||Boolean(errorMessage)||cancelled;
  if(page)$('page-label').textContent=page.returned?`Rows ${page.from+1}–${page.from+page.returned} of ${page.total} · ${visible.length} visible`:`No rows on this page · ${page.total} total`;
  const warnings=[];
  if(payload?.meta?.missing_instruments?.length)warnings.push(`Missing instruments: ${payload.meta.missing_instruments.join(', ')}. These instruments are not included in this snapshot.`);
  if(payload?.meta?.missing_live_quotes?.length)warnings.push(`Current quote unavailable: ${payload.meta.missing_live_quotes.join(', ')}. Reference data is shown for these instruments.`);
  $('warning').hidden=!warnings.length;$('warning').textContent=warnings.join(' ');
  const notes=[];
  if(hasViews)notes.push('Net shows current exposure; Day shows today’s activity. These are separate views and must not be added together. The selector filters this page.');
  if(payload?.meta?.timestamp_note)notes.push(payload.meta.timestamp_note);
  if(payload?.meta?.timezone)notes.push(`Timezone: ${payload.meta.timezone}.`);
  if(args.mode==='historical')notes.push('Chart shows all candles in this snapshot, unaffected by table filters.');
  if(args.mode==='search')notes.push('Instrument-master prices are reference values unless enriched with a timestamped quote. Contract filters apply to this page. Select an instrument for a fresh quote.');
  if(connected&&!app.getHostCapabilities()?.serverTools)notes.push('This host does not support additional requests from the app. Ask for another snapshot in chat.');
  $('note').textContent=notes.join(' ');setBusy();
}
function receive(result,fallbackArgs){
  if(cancelled||acceptedRevision===revision)return;
  const info=result._meta?.['trade.kite/app'];
  // The launcher receives {}, but returns a portfolio snapshot. Its explicit
  // origin correlates that response even when the host omits toolInfo. Only
  // an outstanding empty input may accept a launcher result.
  const launcherResult=info?.origin?.tool==='kite_explorer'&&info.tool==='portfolio'&&
    expectedArgs&&Object.keys(expectedArgs).length===0&&
    requestKey(info.origin.arguments)===requestKey(expectedArgs);
  if(info&&expectedArgs&&!launcherResult&&requestKey(info.arguments||{})!==requestKey(expectedArgs))return;
  if(info&&allowedTools.has(info.tool)){tool=info.tool;args=info.arguments||{};}else if(fallbackArgs)args=fallbackArgs;
  acceptedRevision=revision;waiting=false;errorMessage='';hasResult=true;
  try{
    payload=readResult(result);rows=rowsFor(payload);populateFilters();render();
    const scope=args.mode==='margins'?`${rows.length} account segments`:args.mode==='holdings'&&args.type==='summary'?'Account-wide summary':args.mode==='historical'?`${rows.length} candles`:`${rows.length} rows on this page`;
    status(`Received ${receivedTime()} · ${scope}. Monetary values in INR.`);
    if(tool==='portfolio'&&args.mode==='holdings'){
      if(args.type==='summary')summary.load(payload);
      else if(!summary.data&&panelRequests.canCall())summary.load();
      else if(!summary.data) { $('account-summary').hidden=false;$('account-summary').replaceChildren(el('p','Account-wide totals need a holdings summary. Ask for one in chat.','muted')); }
    }
  }catch(error){payload=null;rows=[];errorMessage=error.message;render();status('Request failed. You can retry below.',true);}
  setBusy();
}
async function call(nextArgs,{refreshSummary=true}={}){
  if(waiting||inFlight||!canCall())return;
  const current=++revision,requestedTool=tool;
  // Retain the attempted page for Retry, including bridge failures with no result.
  refreshSummaryForRequest=refreshSummary;
  args={...nextArgs};expectedArgs=args;
  const operation={arguments:args,controller:new AbortController()};pendingCall=operation;
  cancelled=false;errorMessage='';waiting=true;inFlight=true;payload=null;rows=[];resetPanels(!refreshSummary&&tool==='portfolio'&&args.mode==='holdings');render();status('Requesting data from Kite…');
  try{const result=await app.callServerTool({name:requestedTool,arguments:args},{signal:operation.controller.signal});if(revision===current)receive(result,operation.arguments);}
  catch(error){if(revision===current){waiting=false;hasResult=true;errorMessage=error.message;payload=null;rows=[];render();status(`Could not load snapshot: ${error.message}`,true);}}
  finally{if(pendingCall===operation){pendingCall=null;inFlight=false;setBusy();}}
}
$('refresh-prices').onclick=async()=>{
  if(priceBusy||waiting||inFlight||!canCall())return;
  const instruments=[...new Set(rows.map(row=>instrumentRef(row)?.id).filter(Boolean))];
  if(!instruments.length)return;
  const generation=++priceGeneration;priceBusy=true;priceMessage='Requesting quotes for this page…';render();
  try {
    const quotes=new Map();
    for(let from=0;from<instruments.length;from+=500){
      const result=await panelRequests.run('prices','market',{mode:'quote',instruments:instruments.slice(from,from+500)});
      if(generation!==priceGeneration)return;
      for(const quote of rowsFor(result))if(finite(quote.last_price))quotes.set(instrumentRef(quote)?.id,quote);
    }
    rows=rows.map(row=>{
      const {latest_price,latest_quote_timestamp,...snapshot}=row;
      const quote=quotes.get(instrumentRef(row)?.id);
      return quote?{...snapshot,latest_price:quote.last_price,latest_quote_timestamp:quote.timestamp||quote.quote_timestamp||null}:snapshot;
    });
    const missing=instruments.filter(id=>!quotes.has(id));
    priceMessage=`Quotes received ${receivedTime()} · ${instruments.length-missing.length}/${instruments.length} instruments. Portfolio totals and P&L are unchanged.${missing.length?' Unavailable: '+missing.join(', ')+'.':''}`;
  }catch(error){
    if(generation!==priceGeneration||error.name==='AbortError')return;
    priceMessage=`Price refresh failed: ${error.message}. Any displayed quotes are previous snapshots.`;
  }finally{if(generation===priceGeneration){priceBusy=false;render();}}
};
$('refresh').onclick=()=>call({...args});
$('previous').onclick=()=>payload?.pagination&&call(pageArguments(args,payload.pagination,-1),{refreshSummary:false});
$('next').onclick=()=>payload?.pagination&&call(pageArguments(args,payload.pagination,1),{refreshSummary:false});
$('columns').onclick=()=>{expandedColumns=!expandedColumns;render();};
for(const id of ['filter','view','contract-exchange','contract-type','contract-expiry','position-state','display'])$(id)[id==='filter'?'oninput':'onchange']=render;
$('list-sort').onchange=()=>{sortKey=$('list-sort').value;direction=1;render();};
$('sort-direction').onclick=()=>{direction=-direction;render();};
$('full-contracts').onclick=()=>call({...args,verbosity:'full'});
$('search-submit').onclick=event=>{event.preventDefault();const query=$('finder-query').value.trim();if(query){$('filter').value='';for(const id of ['contract-exchange','contract-type','contract-expiry'])$(id).value='';call({mode:'search',query,filter_on:$('finder-field').value,search_mode:'search',verbosity:'full',from:0,limit:args.limit||100,include_live_quote:$('finder-live').checked});}};
$('finder-query').onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();$('search-submit').click();}};
app.ontoolinput=params=>{
  const nextArgs=params.arguments||{};
  if(panelRequests.ownsInput(nextArgs))return;
  const ownCall=pendingCall&&requestKey(nextArgs)===requestKey(pendingCall.arguments);
  if(!ownCall){++revision;pendingCall?.controller.abort();pendingCall=null;inFlight=false;}
  resetPanels(Boolean(ownCall)&&!refreshSummaryForRequest&&tool==='portfolio'&&nextArgs.mode==='holdings');cancelled=false;errorMessage='';expectedArgs=nextArgs;args=nextArgs;payload=null;rows=[];hasResult=false;waiting=true;
  if(!ownCall){
    refreshSummaryForRequest=true;
    $('position-state').value='';
    $('filter').value='';$('view').value='net';sortKey='';direction=1;expandedColumns=false;
    for(const id of ['contract-exchange','contract-type','contract-expiry'])$(id).value='';
    $('finder-field').value=args.filter_on||'id';
    $('finder-query').value=typeof args.query==='string'?args.query:'';$('finder-live').checked=Boolean(args.include_live_quote);
  }
  render();status('Waiting for Kite data…');
};
app.ontoolresult=result=>{
  // App calls are resolved exclusively by their RPC promise, never by an echo.
  if(inFlight||acceptedRevision===revision)return;
  const info=result._meta?.['trade.kite/app'];
  if(panelRequests.ownsResult(result,expectedArgs)&&(!info||requestKey(info.arguments||{})!==requestKey(expectedArgs||{})))return;
  receive(result);
};
app.ontoolcancelled=()=>{++revision;waiting=false;hasResult=true;cancelled=true;pendingCall?.controller.abort();payload=null;rows=[];errorMessage='';resetPanels();render();status('Request cancelled. Refresh to retry.',true);};
app.onhostcontextchanged=ctx=>hostContext({...app.getHostContext(),...ctx});
app.onteardown=async()=>{++revision;connected=false;waiting=false;pendingCall?.controller.abort();clearTimeout(countTimer);resetPanels();setBusy();return {};};
app.connect().then(()=>{connected=true;hostContext(app.getHostContext());setBusy();}).catch(error=>{errorMessage=`Could not connect to the MCP Apps host: ${error.message}`;hasResult=true;render();status('Connection failed.',true);});
