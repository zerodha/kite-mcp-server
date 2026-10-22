import { el, action, detailList, metric, rangeBar } from './dom.js';
import { instrumentRef, rowIdentity, finite, formatCell, tone, receivedTime, dateIST, historyArguments, rowsFor, holdingMetrics, expiryFor, snapshotTime, holdingQuantity, optionStrike } from './model.js';
import { priceChart } from './chart.js';

export class Inspector {
  constructor(container, requests, onSelection) {
    this.container=container;this.requests=requests;this.onSelection=onSelection;this.tab='overview';this.generation=0;
  }
  clear() {
    ++this.generation;this.requests.cancel('quote');this.requests.cancel('chart');this.chartNode?.dispose();this.chartNode=null;
    this.row=null;this.quote=null;this.history=null;this.historyArgs=null;this.container.replaceChildren();this.container.hidden=true;this.onSelection(null);
  }
  open(row, mode, opener) {
    if(!instrumentRef(row))return;
    this.clear();this.row=row;this.mode=mode;this.opener=opener;this.ref=instrumentRef(row);this.tab='overview';
    this.quoteError='';this.chartError='';this.quoteLoading=false;this.chartLoading=false;this.quoteTime=null;this.historyTime=null;
    this.to=dateIST(); const start=new Date(`${this.to}T00:00:00Z`);start.setUTCDate(start.getUTCDate()-30);this.from=start.toISOString().slice(0,10);this.interval='day';
    // Only market snapshots may seed a quote; holdings.average_price means cost,
    // while quote.average_price means today's average traded price.
    if(['quote','ltp','ohlc'].includes(mode)) { this.quote=row;this.quoteTime='From the main snapshot'; }
    this.container.hidden=false;this.onSelection(rowIdentity(row));this.render();
    this.container.querySelector('h2')?.focus({preventScroll:true});
    if(this.requests.canCall()) this.loadQuote();
  }
  close() {
    const opener=this.opener, key=this.row?rowIdentity(this.row):null;this.clear();
    const target=opener?.isConnected&&opener.getClientRects().length?opener:[...document.querySelectorAll('[data-row-key]')].find(node=>node.dataset.rowKey===key&&node.getClientRects().length);
    target?.focus({preventScroll:true});
  }
  async loadQuote() {
    const generation=this.generation, version=this.quoteVersion=(this.quoteVersion||0)+1;this.quoteLoading=true;this.quoteError='';this.render();
    try {
      const payload=await this.requests.run('quote','market',{mode:'quote',instruments:[this.ref.id]});
      if(generation!==this.generation||version!==this.quoteVersion)return;
      const quote=rowsFor(payload).find(row=>row.instrument===this.ref.id);
      if(!quote)throw new Error('No quote is available for this instrument.');
      this.quote=quote;this.quoteTime=`Received ${receivedTime()}`;
      if(!this.ref.token && Number(quote.instrument_token)>0)this.ref.token=Number(quote.instrument_token);
    } catch(error) { if(generation===this.generation && version===this.quoteVersion && error.name!=='AbortError')this.quoteError=error.message; }
    finally { if(generation===this.generation && version===this.quoteVersion){this.quoteLoading=false;this.render();} }
  }
  async loadHistory() {
    const generation=this.generation, version=this.chartVersion=(this.chartVersion||0)+1;
    let args;
    try {args=historyArguments(this.ref.token,this.from,this.to,this.interval);}catch(error){this.chartError=error.message;this.render();return;}
    this.chartLoading=true;this.chartError='';this.history=null;this.chartNode?.dispose();this.chartNode=null;this.render();
    try {
      const payload=await this.requests.run('chart','market',args);
      if(generation!==this.generation||version!==this.chartVersion)return;
      this.history=rowsFor(payload);this.historyArgs=args;this.historyTime=`Received ${receivedTime()}`;
    } catch(error) {if(generation===this.generation && version===this.chartVersion && error.name!=='AbortError')this.chartError=error.message;}
    finally {if(generation===this.generation && version===this.chartVersion){this.chartLoading=false;this.render();}}
  }
  renderDepth(target) {
    const quote=this.quote;
    if(!quote?.depth) {target.append(el('p',this.quoteLoading?'Loading market depth…':'Market depth is not available in this snapshot.','muted'));return;}
    const sides=el('div',null,'depth-sides');
    for(const [key,label] of [['buy','Bids'],['sell','Offers']]) {
      const levels=(quote.depth[key]||[]).filter(level=>finite(level.price)&&level.price>0&&finite(level.quantity)&&level.quantity>=0).slice(0,5);
      const side=el('section',null,`depth-side ${key}`);side.append(el('h3',label));
      const table=el('table');table.append(el('caption',`${label} · up to five levels`,'sr-only'));
      const head=el('thead'),tr=el('tr');for(const name of ['Price','Orders','Qty']){const th=el('th',name);th.scope='col';tr.append(th);}head.append(tr);table.append(head);
      const body=el('tbody'),max=Math.max(1,...levels.map(level=>level.quantity));
      for(const level of levels) {
        const tr=el('tr');
        for(const [field,format] of [['price','last_price'],['orders','orders'],['quantity','quantity']]) {
          const td=el('td',formatCell(level[field],format));
          if(field==='quantity'){const track=el('span',null,'depth-track'),bar=el('span',null,'depth-bar');track.setAttribute('aria-hidden','true');bar.style.width=`${level.quantity/max*100}%`;track.append(bar);td.append(track);}
          tr.append(td);
        }
        body.append(tr);
      }
      table.append(body);side.append(table);if(!levels.length)side.append(el('p','No orders at this snapshot.','muted'));
      sides.append(side);
    }
    target.append(sides,el('p','Displayed depth is a snapshot of up to five levels, not the entire order book.','muted'));
    const totals=el('div',null,'metric-grid');totals.append(metric('Total buy quantity',quote.buy_quantity,'quantity'),metric('Total sell quantity',quote.sell_quantity,'quantity'));target.append(totals);
  }
  renderOverview(target) {
    const row=this.row,q=this.quote;
    if(this.mode==='holdings') {
      const values=holdingMetrics(row),grid=el('div',null,'metric-grid');
      grid.append(metric('Total quantity',holdingQuantity(row),'quantity'),metric('Avg. cost (₹)',row.average_price,'average_price'),metric('Invested (₹)',values.invested,'invested_amount'),metric('Current (₹)',values.current,'current_value'),metric('P&L (₹)',row.pnl,'pnl',true),metric('Return',values.percent,'return_percent',true));if(finite(row.t1_quantity))grid.append(metric('T1 quantity',row.t1_quantity,'quantity'));target.append(grid,el('p','Holding values are from the portfolio snapshot. Quote prices below are fetched separately.','muted'));
    } else if(this.mode==='positions') {
      const grid=el('div',null,'metric-grid');
      for(const [label,key] of [['Net quantity','quantity'],['Average price (₹)','average_price'],['P&L (₹)','pnl'],['Realised (₹)','realised'],['Unrealised (₹)','unrealised'],['M2M (₹)','m2m']])grid.append(metric(label,row[key],key,['pnl','realised','unrealised','m2m'].includes(key)));
      target.append(el('p',`${row.product||'Product unavailable'} · ${row.view==='day'?'Today’s activity':'Net exposure'} · ${row.quantity===0?'Closed':row.quantity>0?'Long':'Short'}`,'badge-line'),grid);
    }
    if(q) {
      const range=rangeBar(q.ohlc,q.last_price);if(range)target.append(range);
      const grid=el('div',null,'metric-grid');
      for(const [label,key,value] of [['Open (₹)','open',q.ohlc?.open],['Prev. close (₹)','close',q.ohlc?.close],['Volume','volume',q.volume],['Avg. traded price (₹)','average_price',q.average_price],['Open interest','oi',q.oi],['Last quantity','quantity',q.last_quantity],['Lower circuit (₹)','last_price',q.lower_circuit_limit],['Upper circuit (₹)','last_price',q.upper_circuit_limit]])if(value!=null)grid.append(metric(label,value,key));
      target.append(grid);
      if(snapshotTime(q.last_trade_time))target.append(el('p',`Last trade: ${snapshotTime(q.last_trade_time)}`,'muted'));
    } else if(!this.quoteLoading)target.append(el('p','Request a quote to see today’s range and market statistics.','muted'));
    if(this.mode==='search') {
      const info={};for(const key of ['name','exchange','instrument_type','expiry_date','expiry','strike','lot_size','tick_size','active'])if(row[key]!=null&&(key!=='strike'||optionStrike(row)))info[key]=row[key];
      target.append(detailList(info));
    }
  }
  renderHistory(target) {
    const form=el('div',null,'chart-controls');form.setAttribute('role','group');form.setAttribute('aria-label','Historical chart period');
    const input=(label,name,type,value)=>{const field=el('label',label);const node=el('input');node.type=type;node.value=value;node.name=name;node.id=`chart-${name}`;field.append(node);form.append(field);return node;};
    const from=input('From','from','date',this.from),to=input('To','to','date',this.to);
    const label=el('label','Interval'),interval=el('select');interval.name='interval';interval.id='chart-interval';
    for(const [id,text] of [['minute','1 minute'],['5minute','5 minutes'],['15minute','15 minutes'],['60minute','1 hour'],['day','Daily']]){const option=el('option',text);option.value=id;interval.append(option);}interval.value=this.interval;label.append(interval);form.append(label);
    const button=el('button',this.chartLoading?'Loading…':'Load chart');button.type='button';button.id='chart-load';button.disabled=this.chartLoading||!this.requests.canCall()||!this.ref.token;form.append(button);
    const save=()=>{this.from=from.value;this.to=to.value;this.interval=interval.value;};from.onchange=save;to.onchange=save;interval.onchange=save;
    button.onclick=event=>{event.preventDefault();save();this.loadHistory();};target.append(form);
    if(!this.ref.token)target.append(el('p','A chart needs an instrument token. Load a full quote or use instrument search.','muted'));
    if(this.chartError)target.append(el('p',this.chartError,'panel-error'));
    if(this.chartLoading)target.append(el('p','Loading historical candles…','muted'));
    if(this.history) {
      if(!this.chartNode)this.chartNode=priceChart(this.history,`${this.ref.symbol} historical prices`);
      target.append(this.chartNode,el('p',`${this.historyTime} · ${this.historyArgs.from_date} to ${this.historyArgs.to_date} · ${this.historyArgs.interval} · IST`,'muted'));
      const details=el('details');details.append(el('summary','Candle values'));
      const scroll=el('div',null,'scroll'),table=el('table');table.append(el('caption',`${this.ref.symbol} historical candle values`,'sr-only'));
      const head=el('thead'),tr=el('tr');for(const key of ['Date','Open','High','Low','Close','Volume','OI']){const th=el('th',key);th.scope='col';tr.append(th);}head.append(tr);table.append(head);
      const body=el('tbody');for(const row of this.history){const tr=el('tr');for(const key of ['date','open','high','low','close','volume','oi'])tr.append(el('td',formatCell(row[key],key)));body.append(tr);}table.append(body);scroll.append(table);details.append(scroll);target.append(details);
    }
  }
  render() {
    if(!this.row)return;
    // Cache the canvas across tab changes, but disconnect observers on selection changes.
    const focusedNode=this.container.contains(document.activeElement)?document.activeElement:null;
    const focused=focusedNode?.id;
    this.container.replaceChildren();
    const top=el('div',null,'inspector-header'),title=el('h2',this.ref.symbol);title.tabIndex=-1;title.id='inspector-title';
    top.append(title,action('Back to list',()=>this.close(),'quiet'));this.container.append(top);
    const expiry=expiryFor(this.row);
    this.container.append(el('p',[this.ref.exchange,this.row.product,this.row.instrument_type,expiry].filter(Boolean).join(' · '),'muted'));
    const quoteLine=el('div',null,'quote-line');
    const q=this.quote,change=finite(q?.last_price)&&finite(q?.ohlc?.close)?q.last_price-q.ohlc.close:null;
    quoteLine.append(el('strong',q?`₹${formatCell(q.last_price,'last_price')}`:'—','quote-price'));
    if(finite(change))quoteLine.append(el('span',`Day change ${formatCell(change,'price_change')}${q.ohlc.close?` (${formatCell(change/q.ohlc.close*100,'return_percent')})`:''}`,tone(change)));
    const refresh=action(this.quoteLoading?'Loading quote…':'Refresh quote',()=>this.loadQuote(),'quiet');refresh.id='inspector-quote-refresh';refresh.disabled=this.quoteLoading||!this.requests.canCall();quoteLine.append(refresh);this.container.append(quoteLine);
    this.container.append(el('p',this.quoteTime||'Quote not loaded','muted'));
    if(snapshotTime(q?.timestamp||q?.quote_timestamp))this.container.append(el('p',`Quote timestamp: ${snapshotTime(q.timestamp||q.quote_timestamp)}`,'muted'));
    if(this.quoteError)this.container.append(el('p',`${this.quoteError}${q?' Displayed quote is the previous snapshot.':''}`,'panel-error'));
    if(!this.requests.canCall())this.container.append(el('p','This host cannot load additional data. Ask for quotes or historical prices in chat.','muted'));
    const tabs=el('div',null,'inspector-tabs');tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','Instrument details');
    const names=['overview','chart','depth','details'];
    for(const name of names) {
      const button=action(name[0].toUpperCase()+name.slice(1),()=>{
        this.tab=name;this.render();this.container.querySelector(`#inspector-tab-${name}`)?.focus();
        if(name==='chart'&&!this.history&&!this.chartLoading&&this.ref.token&&this.requests.canCall())this.loadHistory();
      });
      button.id=`inspector-tab-${name}`;button.setAttribute('role','tab');button.setAttribute('aria-selected',String(this.tab===name));button.setAttribute('aria-controls','inspector-body');button.tabIndex=this.tab===name?0:-1;
      button.onkeydown=event=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();const index=event.key==='Home'?0:event.key==='End'?names.length-1:(names.indexOf(this.tab)+(event.key==='ArrowRight'?1:-1)+names.length)%names.length;tabs.querySelectorAll('button')[index].click();};
      tabs.append(button);
    }
    this.container.append(tabs);const body=el('div',null,'inspector-body');body.id='inspector-body';body.setAttribute('role','tabpanel');body.setAttribute('aria-labelledby',`inspector-tab-${this.tab}`);body.setAttribute('aria-busy',String(this.tab==='chart'?this.chartLoading:this.quoteLoading));
    if(this.tab==='overview')this.renderOverview(body);
    if(this.tab==='depth')this.renderDepth(body);
    if(this.tab==='chart')this.renderHistory(body);
    if(this.tab==='details'){body.append(el('h3','Source row'),detailList(this.row));if(this.quote)body.append(el('h3','Quote snapshot'),detailList(this.quote));}
    this.container.append(body);
    if(focused)document.getElementById(focused)?.focus({preventScroll:true});
    else if(focusedNode?.isConnected&&this.container.contains(focusedNode))focusedNode.focus({preventScroll:true});
  }
}
