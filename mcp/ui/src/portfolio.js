import { el, action, metric, detailList } from './dom.js';
import { finite, receivedTime, holdingMetrics, formatCell, tone, instrumentRef, rowIdentity, expiryFor, holdingQuantity, optionStrike } from './model.js';

export class PortfolioSummary {
  constructor(container,requests){this.container=container;this.requests=requests;this.version=0;this.data=null;this.loading=false;}
  clear(){this.data=null;this.loading=false;++this.version;this.requests.cancel('summary');this.container.replaceChildren();this.container.hidden=true;}
  async load(seed=null){
    if(this.loading&&!seed)return;
    this.loading=true;const version=++this.version;this.container.hidden=false;this.container.replaceChildren(el('p','Loading account-wide holdings summary…','muted'));
    try {
      const payload=seed||await this.requests.run('summary','portfolio',{mode:'holdings',type:'summary'});
      if(version!==this.version)return;
      const data=payload?.data||payload;
      if(!data||!['invested_amount','current_value','total_pnl','today_pnl'].some(key=>finite(data[key])))throw new Error('An account-wide summary was not returned.');
      this.data=data;
      const heading=el('div',null,'section-heading');heading.append(el('h2','Portfolio overview'),el('span',`Account-wide · ${receivedTime()}`,'muted'));
      const grid=el('div',null,'account-metrics');grid.append(metric('Invested (₹)',data.invested_amount,'invested_amount'),metric('Current value (₹)',data.current_value,'current_value'));
      for(const [label,key,percent] of [['Total P&L (₹)','total_pnl','total_pnl_percent'],['Today’s P&L (₹)','today_pnl','today_pnl_percent']]){
        const item=metric(label,data[key],'pnl',true);
        if(finite(data[percent]))item.append(el('span',formatCell(data[percent],percent),tone(data[percent])));
        grid.append(item);
      }
      this.container.replaceChildren(heading,grid);
      this.container.title='Account totals are fetched separately and are independent of the current page and filters.';
    }catch(error){
      if(version!==this.version||error.name==='AbortError')return;
      const line=el('div',null,'summary-error');line.append(el('span',`Account summary unavailable: ${error.message}`,'muted'));
      if(this.requests.canCall())line.append(action('Retry summary',()=>this.load(),'quiet'));
      this.container.replaceChildren(line);
    }finally { if(version===this.version)this.loading=false; }
  }
}
export function instrumentList(rows,mode,select,selected){
  const list=el('div',null,'instrument-list');list.setAttribute('role','list');list.setAttribute('aria-label',`${mode} instruments`);
  let previousExpiry=null, group=list;
  const ordered=mode==='search'?[...rows].sort((a,b)=>(expiryFor(a)||'9999').localeCompare(expiryFor(b)||'9999')):rows;
  for(const row of ordered){
    if(mode==='search'&&expiryFor(row)!==previousExpiry){
      previousExpiry=expiryFor(row);
      const section=el('div',null,'instrument-group');section.setAttribute('role','listitem');
      const heading=el('h3',previousExpiry?`Expiry · ${previousExpiry}`:'Expiry not supplied','group-label');
      group=el('div');group.setAttribute('role','list');group.setAttribute('aria-label',heading.textContent);
      section.append(heading,group);list.append(section);
    }
    const ref=instrumentRef(row);const wrapper=el('div',null,'instrument-item');wrapper.setAttribute('role','listitem');
    const button=action('',event=>select(row,event.currentTarget),'instrument-row');
    button.disabled=!ref;button.dataset.rowKey=rowIdentity(row);button.setAttribute('aria-controls','inspector');button.setAttribute('aria-expanded',String(Boolean(selected)&&selected===rowIdentity(row)));
    const title=el('span',ref?.symbol||row.tradingsymbol||row.name||'Instrument','row-symbol');
    const exchange=el('span',[ref?.exchange,row.product,row.instrument_type].filter(Boolean).join(' · '),'muted');
    const left=el('span',null,'row-left');left.append(title,exchange);
    const right=el('span',null,'row-right');
    if(mode==='holdings'){
      const metrics=holdingMetrics(row);
      left.append(el('span',`Qty ${formatCell(holdingQuantity(row))} · Avg. ${formatCell(row.average_price,'average_price')}`,'muted'),el('span',`Invested ${formatCell(metrics.invested,'invested_amount')}`,'muted'));
      if(finite(row.t1_quantity)&&row.t1_quantity!==0)left.append(el('span',`Includes T1 ${formatCell(row.t1_quantity)}`,'muted'));
      right.append(el('strong',formatCell(row.pnl,'pnl'),tone(row.pnl)),el('span',formatCell(metrics.percent,'return_percent'),tone(metrics.percent)),el('span',`LTP ${formatCell(row.last_price,'last_price')}`,'muted'));
    }else if(mode==='positions'){
      left.append(el('span',`${row.quantity===0?'Closed':row.quantity>0?'Long':'Short'} · Qty ${formatCell(row.quantity)}`,'badge-line'),el('span',`Avg. ${formatCell(row.average_price,'average_price')}`,'muted'));
      right.append(el('strong',formatCell(row.pnl,'pnl'),tone(row.pnl)),el('span',`Realised ${formatCell(row.realised,'realised')}`,'muted'),el('span',`Unrealised ${formatCell(row.unrealised,'unrealised')}`,'muted'));
    }else if(mode==='search'){
      const expiry=expiryFor(row);
      left.append(el('span',row.name||'','muted'));
      right.append(el('strong',expiry||'Expiry unavailable'),el('span',optionStrike(row)?`Strike ${formatCell(row.strike,'strike')}`:'','muted'),el('span',`Lot ${formatCell(row.lot_size)}`,'muted'));
    }else{
      right.append(el('strong',formatCell(row.last_price,'last_price')));
      const change=finite(row.last_price)&&finite(row.ohlc?.close)?row.last_price-row.ohlc.close:null;
      if(finite(change))right.append(el('span',`${formatCell(change,'price_change')} (${row.ohlc.close?formatCell(change/row.ohlc.close*100,'return_percent'):'—'})`,tone(change)));
    }
    if(finite(row.latest_price)){right.append(el('span',`Latest ${formatCell(row.latest_price,'last_price')}`));if(row.latest_quote_timestamp)right.append(el('span',`As of ${row.latest_quote_timestamp}`,'muted'));}
    button.append(left,right);wrapper.append(button);group.append(wrapper);
  }
  return list;
}
export function fundsView(payload){
  const data=payload?.data||payload,root=el('div',null,'funds-grid');
  for(const [key,name] of [['equity','Equity'],['commodity','Commodity']]){
    const segment=data?.[key];if(!segment)continue;
    const section=el('section',null,'funds-segment');section.append(el('h2',name));
    if(segment.enabled===false){section.append(el('p','This segment is not enabled.','muted'));root.append(section);continue;}
    const available=segment.available||{},used=segment.utilised||{},grid=el('div',null,'metric-grid');
    grid.append(metric('Available margin (₹)',segment.net,'margin'),metric('Cash (₹)',available.cash,'cash'),metric('Collateral (₹)',available.collateral,'cash'),metric('Debits (₹)',used.debits,'cash'));
    section.append(grid);
    const details=el('details');details.append(el('summary','Full funds breakdown'),el('h3','Available'),detailList(available),el('h3','Utilised'),detailList(used));section.append(details);root.append(section);
  }
  return root.children.length?root:null;
}

export { portfolioChart } from './portfolio-charts.js';
