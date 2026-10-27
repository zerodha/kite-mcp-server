import { el, action } from './dom.js';
import { rowsFor, instrumentRef, expiryFor, finite, formatCell, receivedTime, snapshotTime } from './model.js';

const PAGE_LIMIT=500;
export function optionContracts(rows, exchange) {
  const unique=new Map();
  for(const row of rows){
    const ref=instrumentRef(row);
    if(ref&&ref.exchange===exchange&&['CE','PE'].includes(row.instrument_type)&&finite(row.strike)&&row.strike>0&&expiryFor(row))unique.set(ref.id,row);
  }
  return [...unique.values()].sort((a,b)=>expiryFor(a).localeCompare(expiryFor(b))||a.strike-b.strike||a.instrument_type.localeCompare(b.instrument_type));
}
export class OptionChain {
  constructor(container,requests,onSelect=null){
    this.container=container;this.requests=requests;this.onSelect=onSelect;this.query='NIFTY';this.exchange='NFO';this.contracts=[];this.strikes=[];this.quotes=new Map();this.expiry='';this.expiries=[];this.catalog=null;this.generation=0;this.busy=false;this.message='';this.quoteMessage='';this.loaded=false;this.detailsOpen=false;
    this.render();
  }
  clear(){++this.generation;this.requests.cancel('chain-search');this.requests.cancel('chain-quotes');this.busy=false;this.contracts=[];this.strikes=[];this.expiries=[];this.catalog=null;this.quotes.clear();this.loaded=false;this.message='';this.quoteMessage='';this.render();}
  async load(query=this.query,exchange=this.exchange,expiry=''){
    return this.loadCatalog(query,exchange,expiry);
  }
  async show(payload,args={}){
    const data=payload?.data||{};
    const query=args.underlying||data.underlying_name||this.query;
    const exchange=args.exchange||data.exchange||this.exchange;
    const expiry=args.expiry||data.expiry||'';
    // A model may have requested a later page. Restart at zero in that case.
    const seed=(payload?.pagination?.from??args.from??0)===0?payload:null;
    return this.loadCatalog(query,exchange,expiry,seed);
  }
  async loadCatalog(query,exchange,expiry='',seed=null){
    query=String(query).trim().toUpperCase();
    exchange=String(exchange).trim().toUpperCase();exchange=({NSE:'NFO',BSE:'BFO'})[exchange]||exchange;
    if(!query||query.length>100||!['NFO','BFO'].includes(exchange)){this.message='Enter an underlying symbol and select its derivatives exchange.';this.render();return;}
    const generation=++this.generation;this.requests.cancel('chain-search');this.requests.cancel('chain-quotes');this.query=query;this.exchange=exchange;this.busy=true;this.contracts=[];this.strikes=[];this.expiries=[];this.catalog=null;this.quotes.clear();this.loaded=false;this.centerRequested=true;this.message='Loading option chain…';this.quoteMessage='';this.render();
    try {
      const contracts=[],strikes=[],seenStrikes=new Set(),seenContracts=new Set();let from=0,snapshot='',loadedAt='',identity='',selectedExpiry=expiry,catalog=null;
      while(true){
        const args={mode:'option_chain',exchange,underlying:query,from,limit:PAGE_LIMIT};
        if(selectedExpiry)args.expiry=selectedExpiry;
        if(snapshot)args.snapshot=snapshot;
        const payload=seed&&from===0?seed:await this.requests.run('chain-search','market',args);
        if(generation!==this.generation)return;
        const data=payload?.data;
        if(!data||!Array.isArray(data.strikes)||!Array.isArray(data.expiries)||!data.loaded_at||!data.expiry)throw new Error('The server returned an invalid option-chain catalog.');
        const revision=payload.meta?.catalog_revision||data.loaded_at;
        const pageIdentity=JSON.stringify([data.exchange,data.underlying_exchange,data.underlying_name,instrumentRef(data.underlying||{})?.id||null]);
        if(data.exchange!==exchange||(selectedExpiry&&data.expiry!==selectedExpiry))throw new Error('The server returned a different option chain. Reload the chain.');
        if(snapshot&&(revision!==snapshot||data.loaded_at!==loadedAt||pageIdentity!==identity))throw new Error('Instrument catalog changed; reload the option chain to use one consistent snapshot.');
        if(!catalog){catalog=data;snapshot=revision;loadedAt=data.loaded_at;identity=pageIdentity;selectedExpiry=data.expiry;}
        for(const strike of data.strikes){
          if(!finite(strike.strike)||strike.strike<=0||seenStrikes.has(strike.strike)||(!strike.call&&!strike.put))throw new Error('Option-chain pagination repeated an invalid strike. Reload the chain.');
          seenStrikes.add(strike.strike);
          for(const [type,side] of [['CE',strike.call],['PE',strike.put]])if(side){
            const ref=instrumentRef(side);
            if(!ref||ref.exchange!==exchange||side.instrument_type!==type||side.strike!==strike.strike||expiryFor(side)!==selectedExpiry||seenContracts.has(ref.id))throw new Error('The server returned an inconsistent option contract. Reload the chain.');
            seenContracts.add(ref.id);contracts.push(side);
          }
          // The backend's pairing and missing sides are authoritative.
          strikes.push(strike);
        }
        if(!payload.pagination?.has_more)break;
        if(!data.strikes.length)throw new Error('Option-chain pagination did not advance. Reload the chain.');
        from+=data.strikes.length;
      }
      this.contracts=contracts;this.strikes=strikes;this.expiries=[...catalog.expiries];this.expiry=selectedExpiry;this.catalog=catalog;this.loaded=true;
      this.message=`${catalog.underlying_name||query} · ${seenStrikes.size} strikes · ${this.contracts.length} option contracts · ${selectedExpiry}${catalog.expiry_type?' ('+catalog.expiry_type+')':''}. Active contracts from the instrument catalog · updated ${snapshotTime(catalog.loaded_at)||catalog.loaded_at}.`;
      this.busy=false;this.render();
      if(this.contracts.length)await this.refresh();
    }catch(error){if(generation===this.generation&&error.name!=='AbortError'){this.message=`Could not load option chain: ${error.message}`;this.busy=false;this.render();}}
  }
  async refresh(){
    if(this.busy||!this.expiry)return;
    const generation=++this.generation,expiry=this.expiry;
    const instruments=[...new Set(this.contracts.filter(row=>expiryFor(row)===expiry).map(row=>instrumentRef(row).id))];
    if(!instruments.length)return;
    const referenceIds=[this.catalog?.underlying,this.catalog?.future].filter(Boolean).map(row=>instrumentRef(row)?.id).filter(Boolean);
    const requested=[...new Set([...instruments,...referenceIds])];
    this.busy=true;this.quoteMessage='Requesting option quotes…';this.render();
    try{
      const next=new Map();
      for(let from=0;from<requested.length;from+=500){
        const payload=await this.requests.run('chain-quotes','market',{mode:'quote',instruments:requested.slice(from,from+500)});
        if(generation!==this.generation)return;
        for(const quote of rowsFor(payload)){const id=instrumentRef(quote)?.id;if(id&&finite(quote.last_price))next.set(id,quote);}
      }
      this.quotes=next;
      const timestamps=[...new Set([...next.values()].map(q=>q.timestamp||q.quote_timestamp).filter(Boolean))].sort();
      const missing=instruments.filter(id=>!next.has(id));
      this.quoteMessage=`Quotes received ${receivedTime()} · ${instruments.length-missing.length}/${instruments.length} contracts.${missing.length?` ${missing.length} quotes unavailable; shown as —.`:''} Snapshots, not streaming prices.${timestamps.length?` Quote timestamps: ${snapshotTime(timestamps[0])||timestamps[0]}${timestamps.length>1?' to '+(snapshotTime(timestamps.at(-1))||timestamps.at(-1)):''}.`:' Quote timestamps unavailable.'}`;
    }catch(error){if(generation===this.generation&&error.name!=='AbortError')this.quoteMessage=`Quote refresh failed: ${error.message}. Any displayed quotes are previous snapshots.`;}
    finally{if(generation===this.generation){this.busy=false;this.render();}}
  }
  render(){
    const previousScroll=this.container.querySelector('.option-chain .scroll')?.scrollTop||0;
    const root=el('section',null,`option-chain${this.detailsOpen?' chain-details-open':''}`);root.append(el('style',`.option-chain .chain-detail-toggle{display:none}.option-chain thead{position:sticky;top:0;z-index:3;background:var(--color-background-secondary,#f6f7f9)}.option-chain thead th{position:static}.option-chain tr.chain-nearest{background:var(--color-background-secondary,#f6f7f9)}@media(max-width:600px){.option-chain .chain-detail-toggle{display:inline-block}.option-chain:not(.chain-details-open) .chain-detail,.option-chain:not(.chain-details-open) .chain-groups{display:none}.option-chain:not(.chain-details-open) table{table-layout:fixed}.option-chain:not(.chain-details-open) th,.option-chain:not(.chain-details-open) td{text-align:center;padding:12px 8px}}`));root.setAttribute('aria-label','Option chain');
    const controls=el('div',null,'controls');controls.style.marginTop='0';
    const queryLabel=el('label','Underlying symbol'),input=el('input');input.value=this.query;input.placeholder='NIFTY';input.maxLength=100;input.setAttribute('aria-label','Option chain underlying');queryLabel.append(input);
    const exchangeLabel=el('label','Exchange'),exchange=el('select');exchange.setAttribute('aria-label','Option chain exchange');
    for(const name of ['NFO','BFO']){const option=el('option',name);option.value=name;exchange.append(option);}exchange.value=this.exchange;exchangeLabel.append(exchange);
    const search=action(this.busy?'Loading…':'Load chain',()=>this.load(input.value,exchange.value));search.disabled=this.busy||!this.requests.canCall();input.onkeydown=event=>{if(event.key==='Enter'&&!search.disabled){event.preventDefault();this.load(input.value,exchange.value);}};
    controls.append(queryLabel,exchangeLabel,search);root.append(controls);
    const message=el('p',this.message||'Load a derivatives underlying to view its available option contracts.','muted');message.setAttribute('role','status');root.append(message);
    if(this.catalog){
      const references=el('div',null,'metric-grid');
      for(const [label,contract] of [['Underlying',this.catalog.underlying],['Same-expiry future',this.catalog.future]]){
        if(!contract)continue;
        const ref=instrumentRef(contract);if(!ref)continue;
        const quote=this.quotes.get(ref.id),block=el('div',null,'metric');block.append(el('span',`${label} · ${ref.id}`,'muted'),el('strong',quote?formatCell(quote.last_price,'last_price'):'—'));
        if(quote?.timestamp||quote?.quote_timestamp)block.append(el('span',snapshotTime(quote.timestamp||quote.quote_timestamp),'muted'));
        references.append(block);
      }
      if(references.children.length)root.append(references);
    }
    if(this.expiries.length){
      const toolbar=el('div',null,'controls');const label=el('label','Expiry'),select=el('select');select.setAttribute('aria-label','Option chain expiry');select.disabled=this.busy;
      for(const expiry of this.expiries){const option=el('option',expiry);option.value=expiry;select.append(option);}select.value=this.expiry;select.onchange=()=>this.load(this.query,this.exchange,select.value);label.append(select);
      const refresh=action('Refresh quotes',()=>this.refresh(),'quiet');refresh.disabled=this.busy||!this.requests.canCall();const details=action(this.detailsOpen?'Hide quote details':'Show quote details',()=>{this.detailsOpen=!this.detailsOpen;this.render();},'quiet chain-detail-toggle');details.setAttribute('aria-pressed',String(this.detailsOpen));toolbar.append(label,refresh,details);
      const spot=this.quotes.get(instrumentRef(this.catalog?.underlying||{})?.id)?.last_price;
      const nearest=finite(spot)?this.strikes.reduce((best,row)=>!best||Math.abs(row.strike-spot)<Math.abs(best.strike-spot)?row:best,null):null;
      const center=()=>{const scroll=root.querySelector('.scroll'),row=root.querySelector('.chain-nearest');if(scroll&&row)scroll.scrollTop+=row.getBoundingClientRect().top-scroll.getBoundingClientRect().top-scroll.clientHeight/2+row.getBoundingClientRect().height/2;};
      if(nearest)toolbar.append(action('Near underlying',center,'quiet'));
      root.append(toolbar);
      const quoteMessage=el('p',this.quoteMessage||'Quotes have not been loaded.','muted');quoteMessage.setAttribute('role','status');root.append(quoteMessage);
      const table=el('table');table.append(el('caption',`${this.exchange}:${this.query} · ${this.expiry} · prices in INR; OI and volume in upstream units`,'sr-only'));
      const head=el('thead'),group=el('tr',null,'chain-groups');for(const [text,span] of [['Calls',5],['Strike',1],['Puts',5]]){const th=el('th',text);th.colSpan=span;th.scope='colgroup';group.append(th);}const headings=el('tr');for(const [index,text] of ['OI','Volume','Bid','Ask','Call LTP','Strike','Put LTP','Bid','Ask','Volume','OI'].entries()){const th=el('th',text,[4,5,6].includes(index)?'':'chain-detail');th.scope='col';headings.append(th);}head.append(group,headings);table.append(head);
      const body=el('tbody');for(const row of this.strikes){const tr=el('tr',null,nearest?.strike===row.strike?'chain-nearest':'');tr.dataset.strike=String(row.strike);for(const type of ['CE','PE']){
        if(type==='PE'){const strike=el('th',formatCell(row.strike,'strike'),'chain-strike');strike.scope='row';tr.append(strike);}
        const contract=row[type==='CE'?'call':'put'],quote=contract?this.quotes.get(instrumentRef(contract).id):null;
        const priceCell=()=>{const td=el('td');if(contract&&this.onSelect){const button=action(quote?formatCell(quote.last_price,'last_price'):'—',()=>this.onSelect(contract),'symbol-button');button.setAttribute('aria-label',`${instrumentRef(contract).symbol}: ${quote?formatCell(quote.last_price,'last_price'):'quote unavailable'}`);td.append(button);}else td.textContent=quote?formatCell(quote.last_price,'last_price'):'—';if(quote?.timestamp||quote?.quote_timestamp)td.title=`Quote timestamp: ${snapshotTime(quote.timestamp||quote.quote_timestamp)||quote.timestamp||quote.quote_timestamp}`;return td;};
        const bid=quote?.depth?.buy?.[0]?.price,ask=quote?.depth?.sell?.[0]?.price;
        const cells=[el('td',formatCell(quote?.oi,'oi'),'chain-detail'),el('td',formatCell(quote?.volume,'volume'),'chain-detail'),el('td',formatCell(bid,'price'),'chain-detail'),el('td',formatCell(ask,'price'),'chain-detail'),priceCell()];if(finite(spot)&&(type==='CE'?row.strike<spot:row.strike>spot))for(const cell of cells)cell.classList.add('chain-itm');tr.append(...(type==='PE'?[cells[4],cells[2],cells[3],cells[1],cells[0]]:cells));
      }body.append(tr);}table.append(body);const scroll=el('div',null,'scroll');scroll.tabIndex=0;scroll.setAttribute('role','region');scroll.setAttribute('aria-label','Scrollable option chain');scroll.append(table);root.append(scroll);
      root.append(el('p',`All strike pages for the selected expiry are loaded from one instrument catalog snapshot. Missing sides and unavailable quotes display —.${this.onSelect?' Select an LTP to inspect a contract.':''}`,'muted'));
    }else if(this.loaded)root.append(el('div','No option contracts found for this underlying and exchange.','empty'));
    if(!this.requests.canCall())root.append(el('p','This host cannot load additional data. Ask for option quotes in chat.','muted'));
    this.container.replaceChildren(root);
    const scroll=root.querySelector('.scroll');
    if(scroll){
      scroll.scrollTop=previousScroll;
      if(this.centerRequested&&root.querySelector('.chain-nearest')){const row=root.querySelector('.chain-nearest');scroll.scrollTop+=row.getBoundingClientRect().top-scroll.getBoundingClientRect().top-scroll.clientHeight/2+row.getBoundingClientRect().height/2;this.centerRequested=false;}
    }
  }
}
