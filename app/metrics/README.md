# Operational metrics

The existing protected `/admin/<secret>/metrics` endpoint exports
Prometheus metrics when configured. This change adds no public endpoint or
browser telemetry upload. `ADMIN_ENDPOINT_SECRET_PATH` configures the path.

## Feature coverage

| Metric | Labels | What it measures |
| --- | --- | --- |
| `kite_mcp_operations_total` | `tool`, `mode`, `outcome`, `service` | Completed tool requests, Skills discovery, resource reads, approval/OAuth HTTP responses and Kite credential renewals |
| `kite_mcp_operation_duration_seconds` | `tool`, `mode`, `service` | Histogram of elapsed server operation time |
| `kite_mcp_approval_observations_total` | `operation`, `method`, `outcome`, `fallback_reason`, `native_attempted`, `native_completed`, `service` | Observations from the order approval guard, including native continuations and browser submission |
| `kite_mcp_browser_approval_events_total` | `operation`, `event`, `service` | Browser review creation, decline, observed expiry, submission, sign-in and future-order permission transitions |

Tool labels cover `portfolio`, `orders`, `gtt`, `market`, `alerts`,
`mutual_funds`, `session`, `help`, `open_kite_view`, `kite_explorer`,
`orders_read`, and `gtt_read`. Each grouped tool's mode comes from its schema.
The view launcher uses its view as the mode. Help uses an exact embedded catalog
key, including the code-mode recipes. Unknown tool names, modes and Help keys
collapse to `other`; request values never become new label values.

`skills` uses modes `list` and `get`. `resources` uses `app`, `skill` or
`other`, without exposing raw URIs. `approval_http` uses `review` or
`permissions`. `oauth_http` uses `register`, `authorize`, `token`, `callback`,
`discovery`, and `protected_resource`, including HTTP rate-limit rejections.
These count HTTP responses, not unique registrations or successful sign-ins.
Credential renewals use `session` / `renewal`.

Tool outcomes are `success`, `tool_error`, `error`, `timeout`, `cancelled`,
`input_required`, and `partial`. A successful quote response with missing
instruments counts as `partial`. HTTP outcomes are `success`, `redirect`,
`client_error`, and `server_error`. Tool errors returned through `isError=true`
count as errors even when the Go handler returns no error. A successful returned
response remains successful if cancellation races with metrics recording. An
explicit returned cancellation or timeout error is recorded as such.

The existing `tool_calls_total` and `tool_errors_total` daily counters now cover
all registered tool requests, with bounded `tool=<tool>_<mode>` labels and the
existing `unknown` session type. Instrument search retains `tool=market_search`
and is counted once per MCP request. Search-specific counters and daily login
metrics remain available. The new feature counters have no date label; use
Prometheus rates and retention rather than summing daily series.

## Approval semantics

Approval methods are `native`, `browser`, `remembered_permission`, and `none`.
Fallback reasons use the same fixed categories as the order results; an empty
reason is exported as `none`. Native attempted/completed labels are booleans.
An MRTR input request counts as a pending native observation with attempted=true;
the subsequent continuation is another request and observation. A browser link
counts as required; its later HTTP submission is a separate observation.

These counters do not count unique orders or exchange fills. `approved` describes
authorization. Failures before the guard runs, such as expired credentials,
report `unavailable`. Use browser events `submitted` and `failed` to distinguish
the submission result. Status polls and page reloads do not count as new approval
observations or repeat lifecycle events.

Browser event values are `created`, `declined`, `expired`, `submission_started`,
`submitted`, `failed`, `login_started`, `login_verified`, `login_failed`,
`permission_granted`, `permission_revoked`, and `permission_expired`.
Permission events use operation `permissions`; other known operations are
`orders_place`, `orders_modify`, and `orders_cancel`. Unknown categories collapse
to `other`.

Expiry is counted when a request or cleanup observes it, not by a timer at the
exact deadline. Permission expiry after logout or restart is not a separate
revocation event. A browser submission counts once, including failed submissions;
a replay does not submit or increment the event again. Overlapping reviews
can each submit an order, but enabling the same active future-order permission
counts as one grant transition. Sign-in attempts count
separately from successful matching-account callbacks. Invalid or reused callback
attempts count as failed sign-in callbacks.

## Useful queries

Tool error rate, grouped by operation:

```promql
sum by (tool, mode) (
  rate(kite_mcp_operations_total{outcome=~"tool_error|error|timeout|cancelled"}[5m])
)
```

95th-percentile operation latency:

```promql
histogram_quantile(0.95,
  sum by (le, tool, mode) (rate(kite_mcp_operation_duration_seconds_bucket[5m]))
)
```

Browser fallbacks by reason, excluding explicitly requested browser review:

```promql
sum by (fallback_reason) (
  rate(kite_mcp_approval_observations_total{method="browser",outcome="required",fallback_reason!="none"}[15m])
)
```

Submission outcomes and future-order permission changes:

```promql
sum by (event) (
  increase(kite_mcp_browser_approval_events_total{
    event=~"submitted|failed|permission_granted|permission_revoked"
  }[1h])
)
```

## Limits and privacy

Labels contain no account, grant, session or order IDs; client names; symbols;
approval URLs; OAuth credentials; raw Help/resource paths; or error messages.
Known Help catalog keys are static and safe to aggregate. These metrics are
operational aggregates, not a trading audit log or a record of user consent.

Tool duration includes time waiting for a native form. It excludes subsequent
browser review, which happens after the tool returns its link. Browser HTTP
latency and credential renewal latency are measured separately. Values reset
when the process restarts; aggregate replicas in Prometheus.

A successful server response or App resource read does not prove the widget
rendered correctly. Browser-side chart failures, disabled controls, ChatGPT safety
blocks before a tool call, client-side failures without a response, and activity
in the hosted documentation site are not visible to this server. Code mode uses
the same MCP tools and Help recipes; these metrics identify their operations,
not which client computation engine ran them.
