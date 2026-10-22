import { test } from 'node:test';
import assert from 'node:assert/strict';
import { PanelRequests } from '../src/requests.js';

test('a slow panel result cannot overwrite a newer instrument, even if the bridge ignores abort',async()=>{
  const pending=[];
  const requests=new PanelRequests((params,options)=>new Promise(resolve=>pending.push({params,options,resolve})),()=>true);
  const old=requests.run('quote','market',{mode:'quote',instruments:['NSE:INFY']});
  const oldCheck=assert.rejects(old,{name:'AbortError'});
  const current=requests.run('quote','market',{mode:'quote',instruments:['NSE:TCS']});
  assert.equal(pending[0].options.signal.aborted,true);
  pending[1].resolve({structuredContent:{data:{'NSE:TCS':{last_price:3500}}}});
  assert.deepEqual(await current,{data:{'NSE:TCS':{last_price:3500}}});
  pending[0].resolve({structuredContent:{data:{'NSE:INFY':{last_price:1500}}}});
  await oldCheck;
});

test('panel correlation survives completion without absorbing an identical new main query',async()=>{
  let finish,wireArgs;
  const requests=new PanelRequests(params=>{wireArgs=params.arguments;return new Promise(resolve=>finish=resolve);},()=>true);
  const args={mode:'holdings',type:'summary'};
  const promise=requests.run('summary','portfolio',args);
  const result={structuredContent:{total_pnl:1},_meta:{'trade.kite/app':{tool:'portfolio',arguments:wireArgs}}};
  assert.notEqual(wireArgs._app_request_id,undefined);
  assert.equal(requests.ownsInput(wireArgs),true);
  assert.equal(requests.ownsInput(args),false);
  finish(result);await promise;
  assert.equal(requests.ownsInput(wireArgs),true,'late input echoes retain identity');
  requests.reset();assert.equal(requests.ownsResult(result),true);
  const legacy={structuredContent:{total_pnl:1}};
  assert.equal(requests.ownsResult(legacy,{mode:'margins'}),true);
  assert.equal(requests.ownsResult(legacy,args),false,'a new main request can return identical data without metadata');
});

test('new snapshots cancel panel slots and unsupported hosts make no calls',async()=>{
  let finish;
  const requests=new PanelRequests(()=>new Promise(resolve=>finish=resolve),()=>true);
  const promise=requests.run('chart','market',{mode:'historical'});
  const check=assert.rejects(promise,{name:'AbortError'});
  requests.reset();finish({structuredContent:{data:[]}});await check;
  const unsupported=new PanelRequests(()=>{throw new Error('must not call');},()=>false);
  await assert.rejects(unsupported.run('quote','market',{}),/host cannot/);
});

test('cancelled panel echoes remain distinct after a newer selection completes',async()=>{
  const pending=[];
  const requests=new PanelRequests(params=>new Promise(resolve=>pending.push({params,resolve})),()=>true);
  const oldArgs={mode:'quote',instruments:['NSE:INFY']};
  const old=requests.run('quote','market',oldArgs),check=assert.rejects(old,{name:'AbortError'});
  const next=requests.run('quote','market',{mode:'quote',instruments:['NSE:TCS']});
  pending[1].resolve({structuredContent:{data:{'NSE:TCS':{last_price:3500}}}});await next;
  assert.equal(requests.ownsInput(pending[0].params.arguments),true);
  assert.equal(requests.ownsInput(oldArgs),false);
  pending[0].resolve({structuredContent:{data:{'NSE:INFY':{last_price:1500}}}});await check;
  assert.equal(requests.ownsResult({structuredContent:{data:{'NSE:INFY':{last_price:1500}}}},{mode:'margins'}),true);
});

test('successive identical panel calls receive different IDs without modifying caller arguments',async()=>{
  const calls=[],args={mode:'quote',instruments:['NSE:INFY']};
  const requests=new PanelRequests(async params=>{calls.push(params);return {structuredContent:{data:[]}};},()=>true);
  await requests.run('quote','market',args);await requests.run('quote','market',args);
  assert.notEqual(calls[0].arguments._app_request_id,calls[1].arguments._app_request_id);
  assert.equal(Object.hasOwn(args,'_app_request_id'),false);
});

test('very late tagged echoes remain recognizable after response-history eviction',async()=>{
  const calls=[];
  const requests=new PanelRequests(async params=>{calls.push(params);return {structuredContent:{value:calls.length}};},()=>true);
  for(let i=0;i<70;i++)await requests.run('quote','market',{mode:'quote',instruments:[`NSE:S${i}`]});
  assert.equal(requests.ownsInput(calls[0].arguments),true);
  assert.equal(requests.ownsInput({mode:'quote',instruments:['NSE:S0']}),false);
  assert.equal(requests.ownsResult({_meta:{'trade.kite/app':{tool:'market',arguments:calls[0].arguments}}}),true);
});
