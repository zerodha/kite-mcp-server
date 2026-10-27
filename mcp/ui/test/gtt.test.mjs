import test from 'node:test';
import assert from 'node:assert/strict';
import { triggerDistances } from '../src/gtt.js';
test('GTT distance uses current quote, preserves upper and lower trigger direction',()=>{
 const trigger={condition:{last_price:50,trigger_values:[90,110]}};
 assert.deepEqual(triggerDistances(trigger,100),[{price:90,distance:-10},{price:110,distance:10}]);
 assert.deepEqual(triggerDistances(trigger,undefined),[{price:90,distance:null},{price:110,distance:null}]);
 assert.deepEqual(triggerDistances(trigger,0),[{price:90,distance:null},{price:110,distance:null}]);
});
test('quote rerender preserves trigger snapshot receipt time',async()=>{
 const { GTTMonitor }=await import('../src/gtt.js');
 const original=globalThis.document;
 class Node {constructor(){this.children=[];}append(...nodes){this.children.push(...nodes);}replaceChildren(...nodes){this.children=nodes;}}
 globalThis.document={createElement:()=>new Node()};
 try{
  const container=new Node(),monitor=new GTTMonitor(container,{canCall:()=>true}),payload={data:[]};
  monitor.show(payload);monitor.receivedAt='original receipt';monitor.quoteMessage='Quotes received later';monitor.show(payload);
  assert.equal(monitor.receivedAt,'original receipt');
  assert.equal(container.children[1].textContent,'Received original receipt · Read-only trigger snapshots');
  monitor.show({data:[]});assert.notEqual(monitor.receivedAt,'original receipt');
 }finally{globalThis.document=original;}
});
