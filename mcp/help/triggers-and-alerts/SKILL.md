---
name: triggers-and-alerts
description: Review price alerts and GTT triggers without confusing triggers with executions.
---

# Triggers and alerts
Use `gtt({"mode":"list","from":0,"limit":100})` for GTTs and `alerts({"mode":"get","uuids":[]})` for alerts. Page GTT results fully. Ask for history with `alerts({"mode":"get","uuids":["<uuid>"],"history":true})` when investigating a specific alert.

- [Audit protective triggers](audit.md)

A simple price alert is a notification; an ATO alert can place a basket of orders. GTTs are conditional triggers, not guaranteed fills. Mutations retain the server's explicit approval requirements.

For requests to show GTTs or inspect trigger prices interactively, use `open_kite_view({"view":"gtt"})` when available. This view only reads triggers and quotes. Alerts remain available through `alerts`; transaction requests retain the original approval workflow. Without the view tool, use the ordinary GTT read mode.
