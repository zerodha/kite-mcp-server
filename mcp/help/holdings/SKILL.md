---
name: holdings
description: Review holdings, portfolio concentration, and intraday versus net exposure.
---

# Holdings and exposure
Use `portfolio` for demat holdings, positions, margins, and profile. Use `mutual_funds({"mode":"holdings"})` separately for mutual funds.

- [Concentration review](concentration.md)
- [Positions and P&L](positions.md)

A snapshot is not a complete tax, cash-flow, or performance history. Identify the valuation time and unavailable fields.

When the user asks to see holdings, visualize concentration, inspect positions, or view funds, use `open_kite_view` if available with `view=holdings`, `positions`, or `funds`. Holdings and position views include charts scoped to the displayed rows. Use `portfolio` directly for data-only analysis or when the UI tool is unavailable.
