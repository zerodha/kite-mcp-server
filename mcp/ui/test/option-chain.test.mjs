import test from 'node:test';
import assert from 'node:assert/strict';
import {optionContracts} from '../src/option-chain.js';
const rows=[
 {id:'NFO:NIFTYCE',instrument_type:'CE',strike:25000,expiry_date:'2026-10-27'},
 {id:'NFO:NIFTYPE',instrument_type:'PE',strike:25000,expiry_date:'2026-10-27'},
 {id:'NFO:NIFTYFUT',instrument_type:'FUT',strike:0,expiry_date:'2026-10-27'},
 {id:'BFO:SENSEXCE',instrument_type:'CE',strike:25000,expiry_date:'2026-10-27'},
 {id:'NFO:NIFTYNOVCE',instrument_type:'CE',strike:26000,expiry_date:'2026-11-24'},
 {id:'NFO:MISSINGEXPIRY',instrument_type:'CE',strike:25000},
];
test('chain includes only identified options from the chosen exchange and deduplicates',()=>{
 const options=optionContracts([...rows,rows[0]],'NFO');
 assert.equal(options.length,3);
 assert.deepEqual(options.map(row=>row.id),['NFO:NIFTYCE','NFO:NIFTYPE','NFO:NIFTYNOVCE']);
});
import {OptionChain} from '../src/option-chain.js';
class HeadlessChain extends OptionChain {render(){}}
const stamp='2026-10-27T12:00:00+05:30';
function catalog(strikes,hasMore=false,loadedAt=stamp){return {data:{exchange:'NFO',underlying_name:'NIFTY',expiry:'2026-10-27',expiry_type:'weekly',expiries:['2026-10-27','2026-11-24'],loaded_at:loadedAt,strikes},pagination:{from:0,has_more:hasMore}};}
test('dedicated chain API pages every strike with a pinned catalog snapshot and preserves all expiries',async()=>{
 const calls=[];
 const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>{
  calls.push(args);
  if(args.mode==='option_chain')return args.from===0?catalog([{strike:25000,call:rows[0],put:rows[1]}],true):catalog([{strike:26000,call:{...rows[0],id:'NFO:SECONDCE',strike:26000},put:null}]);
  return {data:{'NFO:NIFTYCE':{last_price:100,timestamp:stamp}}};
 }};
 const chain=new HeadlessChain(null,requests);await chain.load(' nifty ','NFO');
 assert.deepEqual(calls.filter(c=>c.mode==='option_chain').map(c=>c.from),[0,1]);
 assert.equal(calls[0].underlying,'NIFTY');assert.equal(calls[0].limit,500);
 assert.equal(calls[1].snapshot,stamp);assert.equal(calls[1].expiry,'2026-10-27');
 assert.equal(chain.contracts.length,3);assert.deepEqual(chain.expiries,['2026-10-27','2026-11-24']);
 assert.match(chain.message,/2 strikes/);assert.ok(!calls.some(c=>c.mode==='search'));
});
test('catalog revision change between pages fails without publishing a mixed chain',async()=>{
 const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>args.from===0?catalog([{strike:25000,call:rows[0],put:null}],true):catalog([{strike:26000,call:rows[4],put:null}],false,'changed')};
 const chain=new HeadlessChain(null,requests);await chain.load();
 assert.match(chain.message,/catalog changed/);assert.deepEqual(chain.contracts,[]);assert.equal(chain.loaded,false);
});
test('model result seeds page zero then loads remaining pages and quotes without fetching page zero again',async()=>{
 const calls=[];
 const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>{calls.push(args);return args.mode==='option_chain'?catalog([{strike:26000,call:{...rows[0],id:'NFO:SECONDCE',strike:26000},put:null}]):{data:{}};}};
 const chain=new HeadlessChain(null,requests);
 await chain.show(catalog([{strike:25000,call:rows[0],put:rows[1]}],true),{mode:'option_chain',exchange:'NFO',underlying:'NIFTY'});
 assert.deepEqual(calls.filter(c=>c.mode==='option_chain').map(c=>c.from),[1]);assert.equal(chain.contracts.length,3);
});
test('later model page restarts at zero and explicit expiry is forwarded',async()=>{
 const calls=[];const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>{calls.push(args);return args.mode==='option_chain'?catalog([{strike:25000,call:rows[0],put:null}]):{data:{}};}};
 const chain=new HeadlessChain(null,requests);const seed=catalog([]);seed.pagination.from=500;
 await chain.show(seed,{underlying:'NIFTY',expiry:'2026-10-27',from:500});
 assert.equal(calls[0].from,0);assert.equal(calls[0].expiry,'2026-10-27');
});
test('partial quote response clears omitted quotes on successful refresh, failure retains previous snapshots',async()=>{
 let fail=false;
 const requests={canCall:()=>true,cancel:()=>{},run:async()=>{
  if(fail)throw new Error('Unavailable');
  return {data:{'NFO:NIFTYCE':{last_price:100}}};
 }};
 const chain=new HeadlessChain(null,requests);chain.contracts=optionContracts(rows,'NFO');chain.expiry='2026-10-27';
 chain.quotes.set('NFO:NIFTYPE',{last_price:99});await chain.refresh();
 assert.equal(chain.quotes.has('NFO:NIFTYPE'),false);assert.match(chain.quoteMessage,/1\/2 contracts/);
 fail=true;await chain.refresh();assert.equal(chain.quotes.get('NFO:NIFTYCE').last_price,100);assert.match(chain.quoteMessage,/previous snapshots/);
});
test('quotes include underlying and same-expiry future and batch no more than 500 identifiers',async()=>{
 const calls=[];const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>{calls.push(args);return {data:Object.fromEntries(args.instruments.map(instrument=>[instrument,{last_price:100}]))};}};
 const chain=new HeadlessChain(null,requests);chain.expiry='2026-10-27';
 chain.contracts=Array.from({length:500},(_,index)=>({...rows[0],id:`NFO:OPTION${index}`,strike:25000+index}));
 chain.catalog={underlying:{id:'NSE:NIFTY50'},future:{id:'NFO:NIFTYFUT'}};
 chain.detailsOpen=true;await chain.refresh();
 assert.deepEqual(calls.map(call=>call.instruments.length),[500,2]);
 assert.equal(chain.quotes.has('NSE:NIFTY50'),true);assert.equal(chain.quotes.has('NFO:NIFTYFUT'),true);assert.equal(chain.detailsOpen,true);
 assert.match(chain.quoteMessage,/500\/500 contracts/);
});
test('seeded cash exchange aliases normalize while server strike pairing and null sides remain authoritative',async()=>{
 for(const [alias,exchange] of [['NSE','NFO'],['BSE','BFO']]){
  const calls=[];const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>{calls.push(args);return {data:{}};}};
  const chain=new HeadlessChain(null,requests),call={...rows[0],id:`${exchange}:OPTIONCE`};
  const seed=catalog([{strike:25000,call,put:null}]);seed.data.exchange=exchange;
  await chain.show(seed,{exchange:alias,underlying:'NIFTY'});
  assert.equal(chain.exchange,exchange);assert.equal(chain.loaded,true);
  assert.equal(chain.strikes[0],seed.data.strikes[0]);assert.equal(chain.strikes[0].put,null);
  assert.equal(chain.contracts[0],call);assert.equal(calls[0].mode,'quote');
 }
});
test('catalog revision token is forwarded separately from loaded_at and changes are rejected',async()=>{
 for(const changed of [false,true]){
  const calls=[];const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>{
   calls.push(args);if(args.mode==='quote')return {data:{}};
   const payload=args.from===0?catalog([{strike:25000,call:rows[0],put:null}],true):catalog([{strike:26000,call:{...rows[0],id:'NFO:SECONDCE',strike:26000},put:null}]);
   payload.meta={catalog_revision:args.from&&changed?'revision-b':'revision-a'};return payload;
  }};
  const chain=new HeadlessChain(null,requests);await chain.load('NIFTY','NSE');
  assert.equal(calls[0].exchange,'NFO');assert.equal(calls[1].snapshot,'revision-a');
  assert.equal(chain.loaded,!changed);if(changed)assert.match(chain.message,/catalog changed/);
 }
});
test('different underlying metadata or invalid option side across pages cannot produce a mixed chain',async()=>{
 for(const invalidSide of [false,true]){
  const requests={canCall:()=>true,cancel:()=>{},run:async(slot,name,args)=>{
   if(args.from===0)return catalog([{strike:25000,call:rows[0],put:null}],true);
   const payload=catalog([{strike:26000,call:{...rows[0],id:'NFO:SECONDCE',strike:26000,instrument_type:invalidSide?'PE':'CE'},put:null}]);
   if(!invalidSide)payload.data.underlying_name='BANKNIFTY';return payload;
  }};
  const chain=new HeadlessChain(null,requests);await chain.load();
  assert.equal(chain.loaded,false);assert.deepEqual(chain.strikes,[]);assert.deepEqual(chain.contracts,[]);
  assert.match(chain.message,invalidSide?/inconsistent option contract/:/catalog changed/);
 }
});
