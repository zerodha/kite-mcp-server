import { test } from 'node:test';
import assert from 'node:assert/strict';
import { portfolioChartData, treemapLayout } from '../src/portfolio-charts.js';

test('treemap rectangles preserve area, fit bounds, and do not overlap',()=>{
 const entries=[{value:70},{value:20},{value:8},{value:2},{value:0},{value:NaN}];
 const boxes=treemapLayout(entries);
 assert.equal(boxes.length,4);
 for(const box of boxes){
  assert.ok(box.x>=0&&box.y>=0&&box.x+box.width<=100.000001&&box.y+box.height<=100.000001);
  assert.ok(Math.abs(box.width*box.height/10000-box.value/100)<1e-9);
 }
 for(let i=0;i<boxes.length;i++)for(let j=i+1;j<boxes.length;j++){
  const a=boxes[i],b=boxes[j];
  assert.ok(Math.min(a.x+a.width,b.x+b.width)-Math.max(a.x,b.x)<1e-9||Math.min(a.y+a.height,b.y+b.height)-Math.max(a.y,b.y)<1e-9);
 }
});
test('holdings chart includes T1 quantity and keeps refreshed quote prices separate',()=>{
 const rows=[{tradingsymbol:'INFY',exchange:'NSE',quantity:2,t1_quantity:1,average_price:100,last_price:120,latest_price:999,pnl:60}];
 assert.equal(portfolioChartData(rows,'holdings')[0].value,360);
 assert.equal(portfolioChartData(rows,'holdings','invested')[0].value,300);
 assert.equal(portfolioChartData([{...rows[0],pnl:-60}],'holdings','pnl')[0].value,60);
 assert.equal(rows[0].last_price,120);
});
test('position P&L preserves losses, zero P&L, products, and source rows',()=>{
 const rows=[{tradingsymbol:'X',exchange:'NSE',product:'MIS',pnl:-10},{tradingsymbol:'X',exchange:'NSE',product:'NRML',pnl:0},{pnl:null}];
 const data=portfolioChartData(rows,'positions');
 assert.deepEqual(data.map(item=>item.value),[-10,0]);
 assert.equal(data[0].row,rows[0]);assert.equal(data[1].row.product,'NRML');
});
