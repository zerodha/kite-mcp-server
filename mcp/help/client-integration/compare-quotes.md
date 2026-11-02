---
description: Compare quote batches in code mode while retaining missing instruments, timestamps, and failed requests.
---

# Compare quotes without losing failures

Use this for a read-only comparison of resolved instruments. Read [Code mode clients](code-mode.md)
first. In Pi, discover the actual signatures with `searchTools` and `describeTool`.
These names assume the configured server is named `kite`.

Run at most two independent quote batches together, with at most 500 instruments
per batch. Change the instrument lists to match the user's request. Check `isError`
inside each batch as well as promise rejection; a successful script does not prove
every quote request succeeded.

```javascript
function read(result) {
  if (result.isError) throw new Error(result.content?.find(x => x.type === "text")?.text ?? "Kite request failed");
  if (result.structuredContent) return result.structuredContent;
  for (const item of result.content ?? []) {
    if (item.type !== "text") continue;
    try { const value = JSON.parse(item.text); if (value && typeof value === "object") return value; } catch {}
  }
  throw new Error("Kite returned no JSON data");
}
const batches = [["NSE:INFY", "NSE:TCS"], ["BSE:INFY"]];
const results = await Promise.allSettled(batches.map(async instruments => {
  const value = read(await tools.mcp__kite__market({ mode: "quote", instruments }));
  return {
    requested: instruments,
    quotes: Object.entries(value.data).map(([instrument, quote]) => ({
      instrument, price: quote.last_price, timestamp: quote.timestamp,
      last_trade_time: quote.last_trade_time
    })),
    missing: value.meta.missing_instruments
  };
}));
text(results.map((result, index) => result.status === "fulfilled"
  ? result.value
  : { requested: batches[index], error: result.reason?.message ?? String(result.reason) }));
```

Keep failures alongside successful batches in the answer. A missing instrument has
no quoted price; do not replace it with zero. Preserve quote and last-trade timestamps
and identify stale or missing timestamps. The script reports prices, not a recommendation
to trade. For more than two batches, run subsequent pairs sequentially.
