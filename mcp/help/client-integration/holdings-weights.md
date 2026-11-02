---
description: Page holdings and calculate weights of reported quantities while exposing missing valuations and duplicate securities.
---

# Calculate holdings weights across pages

Use this for a read-only review of the quantities reported in holdings. Read
[Code mode clients](code-mode.md) first and discover the actual tool names. This
Pi example assumes the server is named `kite`.

The calculation uses `quantity * last_price` for each row. It does not add T1,
pledged, or other quantity fields, and it excludes mutual funds and positions.
Explain that scope to the user. Holdings prices are a snapshot; fetch quotes
separately if the task requires current market prices.

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
let from = 0;
const rows = [];
for (let pageNumber = 0; ; pageNumber++) {
  if (pageNumber >= 100) throw new Error("Page limit reached; analysis is incomplete");
  const page = read(await tools.mcp__kite__portfolio({ mode: "holdings", type: "full", from, limit: 100 }));
  if (!Array.isArray(page.data) || !page.pagination) throw new Error("Unexpected holdings response");
  rows.push(...page.data);
  if (!page.pagination.has_more) break;
  if (!Number.isInteger(page.pagination.returned) || page.pagination.returned <= 0 || page.pagination.returned !== page.data.length) {
    throw new Error("Pagination stopped making progress");
  }
  from += page.pagination.returned;
}
const excluded = [], valued = [], seen = new Set();
for (const row of rows) {
  const key = row.isin || `${row.exchange}:${row.tradingsymbol}`;
  if (seen.has(key)) throw new Error("Duplicate security: inspect rows before combining quantities");
  seen.add(key);
  if (!Number.isFinite(row.quantity) || row.quantity < 0 || !Number.isFinite(row.last_price) || row.last_price <= 0 || !Number.isFinite(row.quantity * row.last_price)) {
    excluded.push({ symbol: row.tradingsymbol, reason: "Missing or invalid quantity/price" });
    continue;
  }
  valued.push({ symbol: row.tradingsymbol, value: row.quantity * row.last_price });
}
const total = valued.reduce((sum, row) => sum + row.value, 0);
if (!Number.isFinite(total)) throw new Error("Valuation overflow; inspect the source data");
text({ quantity_basis: "Reported quantity only; no extra quantity fields added", rows_fetched: rows.length,
  priced_value: total, excluded, weights: valued.map(row => ({ ...row, weight_pct: total > 0 ? 100 * row.value / total : null })).sort((a,b) => b.value-a.value),
  coverage: excluded.length ? "Priced rows only; not the whole portfolio" : "All returned holdings rows" });
```

Weights describe priced rows only when any rows are excluded. An empty or zero-value
portfolio has no defined weights. Duplicate ISINs stop the calculation so the assistant
can inspect overlapping rows before combining quantities. If pagination or another
request fails, report that the analysis is incomplete; do not silently use the rows
fetched so far. This workflow does not rebalance or place orders.
