import { el, action } from './dom.js';
import { finite, formatCell, holdingMetrics, instrumentRef, tone } from './model.js';

// A binary treemap keeps every positive weight proportional to its rectangle.
export function treemapLayout(items, x=0, y=0, width=100, height=100) {
  const values=items.filter(item=>finite(item.value)&&item.value>0).sort((a,b)=>b.value-a.value);
  function split(group,left,top,w,h){
    if(!group.length)return [];
    if(group.length===1)return [{...group[0],x:left,y:top,width:w,height:h}];
    const total=group.reduce((sum,item)=>sum+item.value,0);
    let count=1,part=group[0].value;
    while(count<group.length-1&&Math.abs(total/2-(part+group[count].value))<Math.abs(total/2-part))part+=group[count++].value;
    const ratio=part/total;
    return w>=h?[...split(group.slice(0,count),left,top,w*ratio,h),...split(group.slice(count),left+w*ratio,top,w*(1-ratio),h)]:[...split(group.slice(0,count),left,top,w,h*ratio),...split(group.slice(count),left,top+h*ratio,w,h*(1-ratio))];
  }
  return split(values,x,y,width,height);
}
export function portfolioChartData(rows,mode,basis='current') {
  return rows.flatMap((row,index)=>{
    const metrics=holdingMetrics(row),ref=instrumentRef(row);
    const value=mode==='positions'?row.pnl:basis==='pnl'?Math.abs(row.pnl):metrics[basis];
    return finite(value)&&(mode==='positions'||value>0)?[{row,index,label:ref?.symbol||row.tradingsymbol||row.name||'Instrument',exchange:ref?.exchange||'',value,pnl:row.pnl}]:[];
  });
}
const styles=`
.portfolio-chart { margin:20px 0; border:1px solid var(--color-border-secondary,#e8ebef); border-radius:8px; padding:16px; }
.portfolio-chart>summary { cursor:pointer; font-weight:600; }
.portfolio-chart .chart-toolbar { display:flex; justify-content:space-between; align-items:center; gap:12px; flex-wrap:wrap; margin:16px 0 10px; }
.portfolio-chart .chart-toolbar label { display:flex; flex-direction:row; align-items:center; gap:8px; }
.portfolio-chart .treemap { position:relative; height:260px; margin:12px 0; }
.portfolio-chart .treemap-tile { position:absolute; padding:7px; min-height:0; overflow:hidden; border:2px solid var(--color-background-primary,#fff); border-radius:4px; text-align:left; background:var(--color-background-secondary,#f6f7f9); color:var(--color-text-primary,#252a34); }
.portfolio-chart .treemap-tile.positive { background:color-mix(in srgb,var(--positive,#16805b) 17%,var(--color-background-primary,#fff)); }
.portfolio-chart .treemap-tile.negative { background:color-mix(in srgb,var(--negative,#c43d49) 17%,var(--color-background-primary,#fff)); }
.portfolio-chart .treemap-tile[aria-pressed="true"] { outline:2px solid var(--accent,#b94327); outline-offset:-4px; }
.portfolio-chart .treemap-tile strong,.portfolio-chart .treemap-tile span { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.portfolio-chart .chart-readout { min-height:36px; margin:8px 0; }
.portfolio-chart .pnl-bars { display:grid; gap:12px; margin:16px 0; max-height:420px; overflow:auto; }
.portfolio-chart .pnl-row { display:grid; grid-template-columns:minmax(70px,1fr) minmax(90px,3fr) minmax(90px,1fr); align-items:center; gap:10px; }
.portfolio-chart .pnl-symbol { text-align:left; overflow-wrap:anywhere; }
.portfolio-chart .pnl-track { height:20px; position:relative; background:var(--color-background-secondary,#f6f7f9); border-radius:3px; }
.portfolio-chart .pnl-track::after { content:''; position:absolute; left:50%; top:-3px; bottom:-3px; border-left:1px solid var(--color-border-primary,#ccd1d9); }
.portfolio-chart .pnl-bar { position:absolute; top:3px; height:14px; border-radius:2px; background:var(--positive,#16805b); }
.portfolio-chart .pnl-bar.negative { background:var(--negative,#c43d49); }
.portfolio-chart .pnl-value { text-align:right; font-variant-numeric:tabular-nums; }
@media(max-width:480px) { .portfolio-chart { padding:12px; } .portfolio-chart .treemap { height:230px; } .portfolio-chart .pnl-row { grid-template-columns:70px minmax(65px,1fr) 95px; gap:6px; } }
`;
export function portfolioChart(rows,mode,onSelect=null) {
  if(!['holdings','positions'].includes(mode))return null;
  const root=el('details',null,'portfolio-chart');
  root.append(el('summary',mode==='holdings'?'Holdings allocation':'Position P&L chart'),el('style',styles));
  const toolbar=el('div',null,'chart-toolbar'),scope=el('span',null,'muted');
  const views=[...new Set(rows.map(row=>row.view).filter(Boolean))];
  // Defensive guard: a chart must never silently add day and net positions.
  if(mode==='positions'&&views.length>1){root.append(el('p','Choose Day or Net exposure to chart one position view.','muted'));return root;}
  scope.textContent=`Shown rows on this page${views.length?' · '+(views[0]==='net'?'Net exposure':"Today’s activity"):''}`;
  toolbar.append(scope);root.append(toolbar);
  const body=el('div'),readout=el('p',null,'chart-readout muted');readout.setAttribute('role','status');readout.setAttribute('aria-live','polite');root.append(body,readout);
  function show(item,button){
    for(const other of root.querySelectorAll('[aria-pressed]'))other.setAttribute('aria-pressed',String(other===button));
    readout.textContent=`${item.label}${item.exchange?' · '+item.exchange:''}${item.row.product?' · '+item.row.product:''} · ${mode==='holdings'?`${basis==='current'?'Current value':basis==='invested'?'Invested':'Absolute P&L'} ₹${formatCell(item.value,'current_value')} · `:''}P&L ₹${formatCell(item.pnl,'pnl')}`;
    if(onSelect)onSelect(item.row,button);
  }
  let basis='current';
  function draw(){
    body.replaceChildren();readout.textContent='';
    const data=portfolioChartData(rows,mode,basis);
    if(!data.length){body.append(el('p','No values available for this chart.','muted'));return;}
    if(mode==='holdings'){
      const total=data.reduce((sum,item)=>sum+item.value,0),map=el('div',null,'treemap');map.setAttribute('role','group');map.setAttribute('aria-label','Holdings allocation treemap');
      const tiles=[];
      for(const item of treemapLayout(data)){
        const label=`${item.label} · ${formatCell(item.value,'current_value')} INR · ${formatCell(item.value/total*100,'percentage')} of shown value · P&L ${formatCell(item.pnl,'pnl')} INR`;
        const button=action('',()=>show(item,button),`treemap-tile ${tone(item.pnl)}`);button.title=label;button.setAttribute('aria-label',label);button.setAttribute('aria-pressed','false');
        Object.assign(button.style,{left:`${item.x}%`,top:`${item.y}%`,width:`${item.width}%`,height:`${item.height}%`});
        button.append(el('strong',item.label),el('span',formatCell(item.value/total*100,'percentage')));map.append(button);tiles.push(button);
      }
      map.onkeydown=event=>{const index=tiles.indexOf(document.activeElement);if(index<0)return;const step=['ArrowRight','ArrowDown'].includes(event.key)?1:['ArrowLeft','ArrowUp'].includes(event.key)?-1:0;if(step){event.preventDefault();tiles[(index+step+tiles.length)%tiles.length].focus();}};
      body.append(map,el('p',`Area: ${basis==='pnl'?'absolute P&L':basis==='invested'?'invested amount':'current value'}. Color: gain or loss. Values use the portfolio snapshot, independently of refreshed quotes. Select a tile for exact values.`,'muted'));
    }else{
      const sorted=[...data].sort((a,b)=>b.value-a.value),max=Math.max(...sorted.map(item=>Math.abs(item.value)),1),bars=el('div',null,'pnl-bars');bars.setAttribute('role','list');bars.setAttribute('aria-label','Position profit and loss');
      for(const item of sorted){
        const row=el('div',null,'pnl-row');row.setAttribute('role','listitem');
        const button=action([item.label,item.row.product].filter(Boolean).join(' · '),()=>show(item,button),'pnl-symbol quiet');button.title=[item.exchange,item.row.product].filter(Boolean).join(' · ');button.setAttribute('aria-label',`${item.label} ${item.row.product||''}, P&L ${formatCell(item.value,'pnl')} INR`);button.setAttribute('aria-pressed','false');
        const track=el('div',null,'pnl-track');track.setAttribute('aria-hidden','true');const bar=el('span',null,`pnl-bar ${tone(item.value)}`),width=Math.abs(item.value)/max*50;
        Object.assign(bar.style,{left:`${item.value<0?50-width:50}%`,width:`${width}%`});track.append(bar);row.append(button,track,el('span',formatCell(item.value,'pnl'),`pnl-value ${tone(item.value)}`));bars.append(row);
      }
      body.append(bars,el('p','P&L in INR · zero is the center line. Each product remains a separate position.','muted'));
    }
  }
  if(mode==='holdings'){
    const label=el('label','Area'),select=el('select');select.setAttribute('aria-label','Holdings allocation basis');
    for(const [value,text] of [['current','Current value'],['invested','Invested'],['pnl','Absolute P&L']]){const option=el('option',text);option.value=value;select.append(option);}
    select.onchange=()=>{basis=select.value;draw();};label.append(select);toolbar.append(label);
  }
  draw();return root;
}
