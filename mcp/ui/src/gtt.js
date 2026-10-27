import { el, action, detailList } from './dom.js';
import { rowsFor, formatCell, receivedTime, finite, snapshotTime } from './model.js';

export function triggerDistances(trigger,lastPrice){
  return (trigger.condition?.trigger_values||[]).map(price=>({price,distance:finite(price)&&finite(lastPrice)&&lastPrice>0?(price-lastPrice)/lastPrice*100:null}));
}
export class GTTMonitor {
  constructor(container,requests){this.container=container;this.requests=requests;this.generation=0;this.from=0;this.limit=100;this.quotes=new Map();}
  clear(){++this.generation;this.requests.cancel('gtt-list');this.requests.cancel('gtt-quotes');this.container.replaceChildren();this.container.hidden=true;this.quotes.clear();this.quoteBusy=false;}
  async load(from=0){
    const generation=++this.generation;this.from=from;this.quoteBusy=false;this.requests.cancel('gtt-quotes');this.quotes.clear();this.quoteMessage='';this.container.hidden=false;this.container.replaceChildren(el('p','Loading GTT triggers…','muted'));
    try{const payload=await this.requests.run('gtt-list','gtt_read',{mode:'list',from,limit:this.limit});if(generation!==this.generation)return;this.show(payload);}
    catch(error){if(generation!==this.generation||error.name==='AbortError')return;this.container.replaceChildren(el('p',`GTTs unavailable: ${error.message}`,'panel-error'),action('Retry GTTs',()=>this.load(from),'quiet'));}
  }
  show(payload){
    this.from=payload?.pagination?.from??0;
    if(this.payload!==payload)this.receivedAt=receivedTime();this.payload=payload;this.container.hidden=false;this.container.replaceChildren();const heading=el('div',null,'section-heading');heading.append(el('h2','GTT monitor'),action('Refresh GTTs',()=>this.load(this.from),'quiet'));this.container.append(heading,el('p',`Received ${this.receivedAt} · Read-only trigger snapshots`,'muted'));
    const rows=rowsFor(payload),refresh=action(this.quoteBusy?'Refreshing quotes…':'Refresh trigger prices',()=>this.refreshQuotes(),'quiet');refresh.disabled=this.quoteBusy||!rows.length||!this.requests.canCall();this.container.append(refresh,el('p',this.quoteMessage||'Refresh quotes to compare trigger prices with the latest available price. Stored creation prices are not current quotes.','muted'));
    if(!rows.length)this.container.append(el('p','No GTT triggers on this page.','empty'));
    for(const row of rows){
      const condition=row.condition||{},id=condition.exchange&&condition.tradingsymbol?`${condition.exchange}:${condition.tradingsymbol}`:null,quote=this.quotes.get(id),card=el('article',null,'gtt-card'),line=el('div',null,'section-heading');line.append(el('h3',condition.tradingsymbol||`GTT ${row.id}`),el('span',row.status||'Status unavailable',`trigger-status status-${String(row.status||'').toLowerCase().replace(/[^a-z]/g,'')}`));card.append(line,el('p',[condition.exchange,row.type,`Trigger #${row.id}`].filter(Boolean).join(' · '),'muted'));
      if(quote)card.append(el('p',`Latest ₹${formatCell(quote.last_price,'last_price')} · As of ${snapshotTime(quote.timestamp||quote.quote_timestamp)||'timestamp unavailable'}`,'muted'));
      const triggers=el('ul',null,'trigger-levels');for(const {price,distance} of triggerDistances(row,quote?.last_price)){const item=el('li',`Trigger ₹${formatCell(price,'price')}${finite(distance)?` · ${distance>0?'+':''}${formatCell(distance,'percent')} from quote`:''}`);triggers.append(item);}card.append(triggers);
      for(const order of row.orders||[])card.append(el('p',`${order.transaction_type||''} ${formatCell(order.quantity)} · Limit ₹${formatCell(order.price,'price')} · ${order.product||''}`,'gtt-order'));
      if(row.meta?.rejection_reason)card.append(el('p',row.meta.rejection_reason,'panel-error'));
      const details=el('details');details.append(el('summary','Trigger details'),detailList(row));card.append(details);this.container.append(card);
    }
    const page=payload?.pagination;if(page){const nav=el('div',null,'section-heading'),previous=action('Previous GTTs',()=>this.load(Math.max(0,page.from-this.limit)),'quiet'),next=action('Next GTTs',()=>this.load(page.from+page.returned),'quiet');previous.disabled=page.from<=0;next.disabled=!page.has_more;nav.append(previous,el('span',page.returned?`Triggers ${page.from+1}–${page.from+page.returned} of ${page.total}`:`${page.total} triggers`,'muted'),next);this.container.append(nav);}
  }
  async refreshQuotes(){
    if(this.quoteBusy||!this.requests.canCall())return;const generation=this.generation;this.quoteBusy=true;this.show(this.payload);
    const instruments=[...new Set(rowsFor(this.payload).map(row=>row.condition?.exchange&&row.condition?.tradingsymbol?`${row.condition.exchange}:${row.condition.tradingsymbol}`:null).filter(Boolean))];
    try{if(!instruments.length)return;const result=await this.requests.run('gtt-quotes','market',{mode:'quote',instruments});if(generation!==this.generation)return;this.quotes=new Map(rowsFor(result).filter(row=>finite(row.last_price)).map(row=>[row.instrument,row]));const missing=instruments.filter(id=>!this.quotes.has(id));this.quoteMessage=`Quotes received ${receivedTime()}.${missing.length?` Unavailable: ${missing.join(', ')}.`:''}`;}
    catch(error){if(generation!==this.generation||error.name==='AbortError')return;this.quoteMessage=`Quote refresh failed: ${error.message}. Displayed quotes are previous snapshots.`;}
    finally{if(generation===this.generation){this.quoteBusy=false;this.show(this.payload);}}
  }
}
