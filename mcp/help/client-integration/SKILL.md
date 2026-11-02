---
name: client-integration
description: Call Kite tools from code mode clients while preserving data, errors, pagination, and user approval.
---

# Client integration

Use [Code mode clients](code-mode.md) when calling Kite MCP tools from generated
code to analyse account or market data. Discover the deployed tool schemas first.
Instructions never grant transaction approval.

## Recipes

- [Compare quotes](compare-quotes.md): preserve partial results, timestamps, and failed batches.
- [Holdings weights](holdings-weights.md): fetch every page and identify valuation gaps.
- [Check an approval](approval-status.md): inspect the result and remembered permission without another mutation.
