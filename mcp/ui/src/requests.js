import { requestKey, readResult } from './model.js';

// Compact, non-security fingerprints are only a fallback for hosts that strip
// result metadata. Tagged requests use explicit IDs; no account payloads are
// retained in the notification ledger.
function resultKey(result) {
  const text=requestKey({isError:Boolean(result.isError),structuredContent:result.structuredContent,content:result.content});
  let first=2166136261,second=5381;
  for(let i=0;i<text.length;i++){
    const code=text.charCodeAt(i);
    first=Math.imul(first^code,16777619);second=Math.imul(second,33)^code;
  }
  return `${text.length}:${first>>>0}:${second>>>0}`;
}

// Notification fingerprints outlive request slots: a host may echo input/result
// after the response or after cancellation. Keep a bounded history for this app.
export class PanelRequests {
  constructor(call, canCall) {
    this.call=call; this.canCall=canCall; this.slots=new Map();
    this.results=new Map();this.serial=0;
    this.session=Date.now().toString(36)+'-'+Math.random().toString(36).slice(2);
  }
  ownsInput(args) {
    const id=args?._app_request_id,prefix=`${this.session}-`;
    if(typeof id!=='string'||!id.startsWith(prefix))return false;
    const serial=id.slice(prefix.length);
    return /^\d+$/.test(serial)&&Number(serial)>0&&Number(serial)<=this.serial;
  }
  ownsResult(result,expectedArgs) {
    const info=result._meta?.['trade.kite/app'];
    if(info && ['portfolio','market'].includes(info.tool) && this.ownsInput(info.arguments))return true;
    if(info)return false;
    const origins=this.results.get(resultKey(result));
    // The same payload can legitimately be returned by a new main request.
    return Boolean(origins && (!expectedArgs || !origins.has(requestKey(expectedArgs))));
  }
  cancel(slot) { this.slots.get(slot)?.controller.abort(); this.slots.delete(slot); }
  reset() { for(const slot of this.slots.keys())this.cancel(slot); }
  rememberResult(result,args) {
    const key=resultKey(result),origins=this.results.get(key)||new Set();origins.add(requestKey(args));
    if(origins.size>64)origins.delete(origins.values().next().value);
    this.results.set(key,origins);
    if(this.results.size>64)this.results.delete(this.results.keys().next().value);
  }
  async run(slot,name,args) {
    if(!this.canCall())throw new Error('This host cannot request additional data. Ask for it in chat.');
    if(!['portfolio','market'].includes(name))throw new Error('Only read-only portfolio and market tools are supported.');
    this.cancel(slot);
    const wireArgs={...args,_app_request_id:`${this.session}-${++this.serial}`};
    const operation={controller:new AbortController()};
    this.slots.set(slot,operation);
    try {
      const result=await this.call({name,arguments:wireArgs},{signal:operation.controller.signal});
      // Remember even a cancelled response so a late metadata-free echo is safe.
      this.rememberResult(result,args);
      if(this.slots.get(slot)!==operation||operation.controller.signal.aborted)throw new DOMException('Superseded request','AbortError');
      return readResult(result);
    }finally { if(this.slots.get(slot)===operation)this.slots.delete(slot); }
  }
}
