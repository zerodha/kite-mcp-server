import { el, action, detailList } from './dom.js';
import { rowsFor, formatCell, receivedTime, snapshotTime } from './model.js';

// These views use a server-enforced read-only alias, never the trading tool.
export class OrderBook {
  constructor(container,requests){this.container=container;this.requests=requests;this.generation=0;this.from=0;this.limit=100;this.busy=false;}
  clear(){++this.generation;this.requests.cancel('order-book');this.requests.cancel('order-history');this.requests.cancel('order-fills');this.container.replaceChildren();this.container.hidden=true;this.busy=false;this.activeDetail=null;}
  async load(from=0){
    const generation=++this.generation;this.from=from;this.busy=true;this.container.hidden=false;
    this.container.replaceChildren(el('p','Loading today’s orders…','muted'));
    try{const payload=await this.requests.run('order-book','orders_read',{mode:'list',from,limit:this.limit});if(generation!==this.generation)return;this.show(payload);}
    catch(error){if(generation!==this.generation||error.name==='AbortError')return;this.container.replaceChildren(el('p',`Orders unavailable: ${error.message}`,'panel-error'),action('Retry orders',()=>this.load(from),'quiet'));}
    finally{if(generation===this.generation)this.busy=false;}
  }
  show(payload){
    this.from=payload?.pagination?.from??0;
    this.container.hidden=false;this.payload=payload;this.container.replaceChildren();
    const heading=el('div',null,'section-heading');heading.append(el('h2','Order book'),action('Refresh orders',()=>this.load(this.from),'quiet'));this.container.append(heading,el('p',`Today’s orders · Received ${receivedTime()} · Read-only`,'muted'));
    const rows=rowsFor(payload);if(!rows.length)this.container.append(el('p','No orders on this page.','empty'));
    const list=el('div',null,'order-book');
    for(const row of rows){
      const card=el('article',null,'order-card');const line=el('div',null,'section-heading');
      line.append(el('h3',row.tradingsymbol||row.order_id||'Order'),el('span',row.status||'Status unavailable',`order-status status-${String(row.status||'').toLowerCase().replace(/[^a-z]/g,'')}`));
      card.append(line,el('p',[row.transaction_type,row.exchange,row.product,row.order_type].filter(Boolean).join(' · '),'muted'));
      card.append(el('p',`${formatCell(row.filled_quantity)} / ${formatCell(row.quantity)} filled · Avg. ₹${formatCell(row.average_price,'average_price')} · Limit ₹${formatCell(row.price,'price')}`));
      if(row.status_message||row.status_message_raw)card.append(el('p',row.status_message||row.status_message_raw,'panel-error'));
      const details=el('details');details.append(el('summary','Order details'),detailList(row));card.append(details);
      if(row.order_id){const controls=el('div',null,'quote-actions'),body=el('div',null,'order-detail');controls.append(action('Timeline',()=>this.details(row.order_id,'history',body),'quiet'),action('Fills',()=>this.details(row.order_id,'trades',body),'quiet'));card.append(controls,body);}
      list.append(card);
    }
    this.container.append(list);
    const page=payload?.pagination;if(page){const nav=el('div',null,'section-heading');const previous=action('Previous orders',()=>this.load(Math.max(0,page.from-this.limit)),'quiet'),next=action('Next orders',()=>this.load(page.from+page.returned),'quiet');previous.disabled=page.from<=0;next.disabled=!page.has_more;nav.append(previous,el('span',page.returned?`Orders ${page.from+1}–${page.from+page.returned} of ${page.total}`:`${page.total} orders`,'muted'),next);this.container.append(nav);}
  }
  async details(id,mode,body){
    // Keep one detail request active. Switching cards explicitly clears the
    // previous loading state rather than abandoning a spinner on that card.
    this.requests.cancel('order-history');this.requests.cancel('order-fills');
    if(this.activeDetail&&this.activeDetail!==body&&this.activeDetail.isConnected)this.activeDetail.replaceChildren(el('p','Select Timeline or Fills to load this order.','muted'));
    this.activeDetail=body;
    const generation=this.generation,version=(this.detailVersion||0)+1;this.detailVersion=version;
    const slot=mode==='history'?'order-history':'order-fills';body.replaceChildren(el('p',mode==='history'?'Loading timeline…':'Loading fills…','muted'));
    try{const payload=await this.requests.run(slot,'orders_read',{mode,order_id:id});if(generation!==this.generation||version!==this.detailVersion||!body.isConnected)return;
      const rows=rowsFor(payload);body.replaceChildren(el('h4',mode==='history'?'Order timeline':'Order fills'));
      if(!rows.length){body.append(el('p',mode==='history'?'No history returned.':'No fills returned.','muted'));return;}
      const list=el('ol',null,'order-timeline');for(const row of rows){const item=el('li');item.append(el('strong',mode==='history'?(row.status||'Update'):`${formatCell(row.quantity)} @ ₹${formatCell(row.average_price??row.price,'price')}`));item.append(el('p',snapshotTime(row.exchange_update_timestamp||row.order_timestamp||row.fill_timestamp||row.exchange_timestamp)||'Timestamp unavailable','muted'));if(row.status_message||row.status_message_raw)item.append(el('p',row.status_message||row.status_message_raw));const detail=el('details');detail.append(el('summary','Full event'),detailList(row));item.append(detail);list.append(item);}body.append(list);
    }catch(error){if(generation!==this.generation||version!==this.detailVersion||!body.isConnected||error.name==='AbortError')return;body.replaceChildren(el('p',error.message,'panel-error'),action('Retry',()=>this.details(id,mode,body),'quiet'));}
  }
}
