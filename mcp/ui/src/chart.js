import { el } from './dom.js';
import { finite, formatCell, tone } from './model.js';

export function priceChart(rows, label = 'Historical prices') {
  const points = rows.filter(row => finite(row.close)).map(row => ({ ...row, date:row.date || row.timestamp || '' }));
  const root = el('section', null, 'price-chart');
  if (!points.length) { root.append(el('p', 'No price candles returned for this period.', 'muted')); root.dispose = () => {}; return root; }
  const first = points[0], last = points.at(-1), change = last.close - first.close;
  const heading = el('div', null, 'chart-heading');
  const value = el('div', null, 'chart-value');
  value.append(el('strong', `₹${formatCell(last.close, 'close')}`), el('small', `Period change ${formatCell(change, 'price_change')}${first.close !== 0 ? ` (${formatCell(change / first.close * 100, 'return_percent')})` : ''}`, tone(change)));
  const type = el('select'); type.setAttribute('aria-label', 'Chart style');
  for (const [id, text] of [['candles','Candles'],['line','Close line']]) { const option = el('option', text); option.value = id; type.append(option); }
  const fullCandles = points.every(p => [p.open,p.high,p.low,p.close].every(finite) && p.low <= Math.min(p.open,p.close) && p.high >= Math.max(p.open,p.close));
  if (!fullCandles) type.value = 'line';
  type.options[0].disabled = !fullCandles;
  heading.append(value, type); root.append(heading,el('p',`Since first close · ${first.date}`,'muted'));
  const readout = el('p', null, 'chart-readout');
  const canvas = el('canvas', null, 'candle-canvas'); canvas.tabIndex = 0; canvas.setAttribute('role', 'img');
  canvas.setAttribute('aria-label', `${label}. ${points.length} candles, ${first.date} to ${last.date}. Use Left and Right arrow keys to inspect values. Full candle values follow the chart.`);
  let active = points.length - 1, width = 300;
  const height = 250;
  function describe() {
    const p = points[active];
    readout.textContent = `${p.date} · ${['open','high','low','close','volume','oi'].filter(key=>finite(p[key])).map(key=>`${({open:'O',high:'H',low:'L',close:'C',volume:'Vol',oi:'OI'})[key]} ${formatCell(p[key],key)}`).join(' · ')}`;
  }
  function draw() {
    const bounds = canvas.getBoundingClientRect();
    if (!bounds.width) return;
    width = bounds.width;
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.round(width * ratio); canvas.height = height * ratio;
    const ctx = canvas.getContext('2d'); if (!ctx) return;
    ctx.scale(ratio, ratio);
    const style = getComputedStyle(document.documentElement);
    // Resolve host variables through CSS before assigning canvas colors;
    // custom properties can contain light-dark() or nested var() values.
    const probe=el('span');probe.hidden=true;root.append(probe);
    const color=(token,fallback)=>{probe.style.color=`var(${token},${fallback})`;return getComputedStyle(probe).color;};
    const muted=color('--color-text-secondary','#737373');
    const positive=color('--positive','#2e7d32');
    const negative=color('--negative','#c44742');
    const accent=color('--accent','#ff5722');
    probe.remove();
    const left = 8, right = width - 72, top = 12, bottom = 168;
    const lows = points.map(p=>type.value==='candles' ? p.low : p.close);
    const highs = points.map(p=>type.value==='candles' ? p.high : p.close);
    const low = Math.min(...lows), high = Math.max(...highs), pad = (high - low || Math.max(Math.abs(high)*.01, 1))*.08;
    const min = low-pad, max=high+pad, xStep=(right-left)/points.length;
    const x = index => left + (index+.5)*xStep;
    const y = value => bottom-(value-min)/(max-min)*(bottom-top);
    ctx.font = `11px ${style.fontFamily}`; ctx.fillStyle=muted;
    for (let i=0;i<4;i++) {
      const price=min+(max-min)*i/3, yy=y(price);
      ctx.globalAlpha=.16; ctx.strokeStyle=muted; ctx.beginPath();ctx.moveTo(left,yy);ctx.lineTo(right,yy);ctx.stroke();ctx.globalAlpha=1;
      ctx.fillText(formatCell(price,'close'),right+8,yy+4);
    }
    if (type.value==='candles') {
      points.forEach((p,i)=> {
        ctx.strokeStyle=ctx.fillStyle=p.close>=p.open ? positive : negative;
        ctx.beginPath();ctx.moveTo(x(i),y(p.high));ctx.lineTo(x(i),y(p.low));ctx.stroke();
        const body=Math.max(1,Math.min(12,xStep*.65));
        ctx.fillRect(x(i)-body/2,Math.min(y(p.open),y(p.close)),body,Math.max(1,Math.abs(y(p.open)-y(p.close))));
      });
    } else {
      ctx.strokeStyle=accent;ctx.lineWidth=1.8;ctx.beginPath();
      points.forEach((p,i)=>i ? ctx.lineTo(x(i),y(p.close)) : ctx.moveTo(x(i),y(p.close)));ctx.stroke();ctx.lineWidth=1;
      if(points.length===1) {ctx.fillStyle=accent;ctx.beginPath();ctx.arc(x(0),y(first.close),3,0,Math.PI*2);ctx.fill();}
    }
    const maxVolume=Math.max(0,...points.map(p=>finite(p.volume)&&p.volume>=0?p.volume:0));
    if(maxVolume>0) {
      ctx.globalAlpha=.5;
      points.forEach((p,i)=>{ if(!finite(p.volume)||p.volume<0)return;ctx.fillStyle=p.close>=(p.open??p.close)?positive:negative;ctx.fillRect(x(i)-Math.max(1,xStep*.65)/2,223-p.volume/maxVolume*35,Math.max(1,xStep*.65),p.volume/maxVolume*35); });
      ctx.globalAlpha=1;ctx.fillStyle=muted;ctx.fillText('Volume',right+8,205);
    }
    ctx.fillStyle=muted;ctx.fillText(String(first.date).slice(0,10),left,245);
    const end=String(last.date).slice(0,10);ctx.fillText(end,Math.max(left,right-ctx.measureText(end).width),245);
    ctx.setLineDash([3,4]);ctx.strokeStyle=muted;ctx.globalAlpha=.6;ctx.beginPath();ctx.moveTo(x(active),top);ctx.lineTo(x(active),223);ctx.stroke();ctx.globalAlpha=1;ctx.setLineDash([]);
    describe();
  }
  canvas.onpointermove = event => { const rect=canvas.getBoundingClientRect();active=Math.max(0,Math.min(points.length-1,Math.floor((event.clientX-rect.left-8)/(width-80)*points.length)));draw(); };
  canvas.onpointerdown=canvas.onpointermove;
  canvas.onkeydown = event => {
    if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
    event.preventDefault(); active=event.key==='Home'?0:event.key==='End'?points.length-1:Math.max(0,Math.min(points.length-1,active+(event.key==='ArrowRight'?1:-1)));
    readout.setAttribute('aria-live','polite');draw();
  };
  canvas.onblur=()=>readout.removeAttribute('aria-live');
  type.onchange=draw;
  root.append(readout, canvas, el('p', 'Prices in INR · Equal candle spacing · Volume where available', 'muted'));
  const observer=new ResizeObserver(draw);observer.observe(canvas);
  const themeObserver=new MutationObserver(draw);themeObserver.observe(document.documentElement,{attributes:true,attributeFilter:['style','data-theme']});
  root.dispose=()=>{observer.disconnect();themeObserver.disconnect();};
  document.fonts?.ready.then(()=>{if(root.isConnected)draw();});
  describe(); return root;
}
