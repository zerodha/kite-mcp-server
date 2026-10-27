import test from 'node:test';
import assert from 'node:assert/strict';
import { OrderBook } from '../src/orders.js';

class Node {
 constructor(){this.children=[];this.isConnected=true;this.textContent='';}
 append(...nodes){this.children.push(...nodes);}
 replaceChildren(...nodes){this.children=nodes;}
}
test('switching order details cancels old request and clears its loading state',async()=>{
 const original=globalThis.document;globalThis.document={createElement:()=>new Node()};
 try{
  const calls=[],cancelled=[];
  const requests={cancel:slot=>cancelled.push(slot),run:(slot,name,args)=>new Promise(resolve=>calls.push({slot,name,args,resolve}))};
  const orders=new OrderBook(new Node(),requests),first=new Node(),second=new Node();
  const a=orders.details('one','history',first);
  assert.equal(first.children[0].textContent,'Loading timeline…');
  const b=orders.details('two','trades',second);
  assert.equal(first.children[0].textContent,'Select Timeline or Fills to load this order.');
  assert.equal(second.children[0].textContent,'Loading fills…');
  calls[0].resolve({data:[]});await a;
  assert.equal(first.children[0].textContent,'Select Timeline or Fills to load this order.','late old response cannot restore old details');
  calls[1].resolve({data:[]});await b;
  assert.equal(second.children[0].textContent,'Order fills');
  assert.equal(second.children[1].textContent,'No fills returned.');
  assert.deepEqual(cancelled,['order-history','order-fills','order-history','order-fills']);
  assert.deepEqual(calls.map(call=>[call.name,call.args.mode]),[['orders_read','history'],['orders_read','trades']]);
 }finally{globalThis.document=original;}
});
