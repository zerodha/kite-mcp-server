# Kite explorer visual and interaction QA

Date: 2026-10-23

final result: passed

## Target and scope

Adapt the official Kite mobile portfolio and depth patterns, plus Terminal Mode's linked panels, to the existing read-only MCP App. This is an adaptation to a chat embed, not a pixel-for-pixel terminal clone. Keep the existing host fonts/colors, snapshot semantics, and portfolio/market tool bridge.

Sources inspected:

- https://kite.trade/docs/kite/kite-mobile/portfolio/ (published holdings demo)
- https://kite.trade/docs/kite/kite-mobile/marketdepth/ (published instrument/depth/range demo)
- https://zerodha.com/z-connect/business-updates/introducing-kite-terminal-mode (published terminal screenshot)

## Visual evidence

The deterministic browser host captures the bundled app at device scale 1, including 444 × 904 light mobile, 390 × 1050 dark mobile, and 1180 × 1050 desktop. Screenshots and comparisons are generated under the ignored `test-artifacts/` directory.

- `comparison-holdings.png`: source and implementation side by side at 444 px wide. Both place account summary above a grouped two-column instrument list. The implementation adds page-scoped totals, source timing, and explicit pagination because MCP snapshots may be partial. Fixture account values intentionally differ from the public reference.
- `comparison-inspector.png`: published mobile instrument sheet and the implementation's Overview. Both expose quote price, day range, and grouped statistics. The implementation puts depth and historical charts in separate tabs and retains portfolio cost/P&L with a separate quote timestamp. The source has one long sheet; these are intentional interaction differences, not geometry-equivalent screenshots.
- `comparison-terminal.png`: terminal source normalized to 1180 px width above the desktop linked inspector. The app preserves list/detail adjacency and panel tabs; it omits the full-screen workspace editor, order entry, option chain, and external widgets.
- Additional states: `explorer-depth-desktop.png`, `explorer-chart-desktop.png`, `explorer-contract-desktop.png`, `explorer-funds-desktop.png`, `explorer-holdings-mobile-dark.png`, `explorer-inspector-mobile-dark.png`, `explorer-positions-mobile-dark.png`, `loading-mobile.png`, and `error-mobile.png`.

## Findings resolved

- Mobile controls displaced the list too far down: condensed the account summary and moved display/sort controls into a disclosure.
- The inspector initially discarded focus when refreshed or when returning to a different responsive layout: restore stable control focus and the visible matching instrument row.
- Native form submission failed in scripts-only host sandboxes: search and historical loading now use explicit button actions, verified without `allow-forms`.
- Bid/offer text was too small at desktop panel width: increased depth typography and verified both sides remain readable.
- Mixed portfolio and market prices could look synchronized: cost/current values retain their portfolio origin; quote receipt/source timestamps and historical receipt/period are shown independently.

## Required visual surfaces

- Typography: host/system sans; financial values use tabular numerals and consistent monetary decimals. Long contract names wrap without document overflow.
- Spacing: grouped row separators, summary above rows, left/right panel separation on desktop, single focused inspector on mobile.
- Colors: host background/text variables; explicit light/dark and system-dark fallbacks; positive/negative values include signs, not color alone.
- Images: no decorative or remote assets are introduced. Canvas renders actual returned OHLC/volume data; range and depth represent supplied quote values.
- Copy: account-wide versus shown-row totals, reference versus quoted search prices, unavailable data, read-only snapshots, and Net/Day separation are explicit.

## Functional evidence and limitations

`npm test`, both browser suites, and the targeted Go MCP Apps integration tests pass. Browser checks cover primary and auxiliary errors, failed-page retry, independent totals, stale/cancelled replies, echoed notifications, depth, keyboard candle inspection, manual dates/intervals, responsive Back/focus, product/status filters, contract filters/search, disabled account segments, escaped text, and hosts without `serverTools`. The browser tests assert no direct network requests and no page errors.

Verification uses a deterministic SDK host and fixture account data. Native Claude/ChatGPT layout, permissions, and tool-notification behavior still require manual validation in those clients. No live account data or trading actions were used.

## Claude review remediation

The follow-up review fixes were validated with the actual rebuilt bundle:

- Retain panel input/result fingerprints after completion/cancellation, including stripped-metadata echoes. Main app-call responses are consumed through the RPC promise once per revision, eliminating duplicate summary loads.
- Reuse the account summary when paging (including failed-page retry). Explicit Refresh fetches exactly one new summary; the old timestamp remains unchanged when reused.
- Preserve four-decimal prices, depth and price changes. Distinguish day change from chart-period change and label the latter's starting close.
- Add an explicit server search-field control and preserve chat's `filter_on`. Search fixtures now apply the same literal field matching as the server and assert returned rows.
- Remove currency labels/tooltips from mixed profile field/value columns. Also fix a list-display preference hiding later non-instrument tables.
- Include settled plus T1 quantity in derived holding values; show T1 separately and keep settled/T1 table columns visible. Verified against Kite's portfolio API field definitions and its developer-support guidance: https://kite.trade/forum/discussion/13001/t1-t2-and-realized-qty.
- Hide non-option/zero strikes in presentation views; retain original values in source Details.
- Give expiry groups valid nested list/listitem semantics, use expanded state for inspector launchers, debounce filter announcements, and preserve an explicitly chosen disclosure state when resizing.
- Replace native depth meters with consistent decorative bars alongside visible quantity values. Let large summary totals reflow across columns rather than break within numbers.

Post-fix evidence includes `review-currency-depth-mobile.png` and `review-summary-tablet.png`. Both browser suites, model/request tests, targeted Go integration tests, and whitespace checks pass. Additional browser coverage includes profile, LTP, OHLC, summary-only payloads, T1 holdings, delayed echoed inputs and metadata-free results, exactly-once summary loading, and large totals at 700 px.

## Second review remediation

- Replaced argument-only input matching with per-panel correlation IDs, carried through the optional `_app_request_id` read-tool argument. This fixes a legitimate later chat request being swallowed when it repeats an earlier panel query. Tagged echoes remain recognizable after response-history eviction. Ordinary callers do not need the argument.
- Metadata-free response matching accounts for the expected main query, so an identical payload can legitimately satisfy a new main request. The fallback ledger uses compact fingerprints rather than retaining full quote/candle payloads.
- Historical captions now use the query that actually produced the displayed candles, not edited draft controls. Impossible dates such as 30 February are rejected.
- Regression tests cover repeated panel/chat quote and summary queries, stripped metadata with identical data, very late echoes after 70 panel calls, unchanged caller arguments, and draft chart-period edits. The full `go test ./mcp -count=1` package also passes, including a correlation-ID round trip through the authenticated read handler.

## Additional read-only widgets (2026-10-27)

The view navigation now includes Holdings, Positions, Funds, Orders, GTT, and
Option chain. Holdings allocation is a collapsible treemap with Current value,
Invested, and Absolute P&L area choices. Position P&L bars use only the selected
Day or Net view and retain separate product labels. Both charts use filtered
rows on the current page; they do not represent the entire account unless all
holdings or positions fit that page.

Orders show status, rejection reasons, timeline, and fills. GTT shows trigger
legs and optional quote-relative distance. Their `orders_read` and `gtt_read`
server tools accept read modes only and remain hidden from non-Apps clients.
Option chains load backend-indexed NFO/BFO contracts for an underlying, fetch
every strike page of a selected expiry from one pinned catalog revision, and
quote in batches of at most 500. Catalog changes and missing quotes are identified. Mobile defaults to call
LTP, strike, and put LTP; full quote details are available through a toggle.

Validation uses sample data and the local mock host, not a live account:

- `npm test`: chart geometry/data, option discovery/partial responses, order
  detail cancellation, GTT distances/receipt times, bridge request correlation.
- `npm run test:browser`: existing app behavior plus widget navigation, chart
  scope/state, order history/fills, GTT quote distances, compact/full mobile
  chains, overflow, and absence of direct network requests.
- `go test ./...`: server tests, including read-only mode enforcement.

Visual captures are generated in ignored `test-artifacts/widget-*.png`.

Large-chain visual verification uses 601 strikes (1,202 option contracts plus a
spot reference), two catalog pages, and three quote batches. The browser checks
that the last strike is reachable, both sticky header rows remain separate,
and desktop/mobile can jump to the nearest strike to the underlying quote.
The initial quoted view centers that strike; later renders preserve table
scroll position. Fixtures are synthetic and do not represent live market data.

## Kite styling adaptation

`src/kite.css` adapts the local kite-front-js Open Sans typography, 3px control
radii, orange underline tabs, neutral table rules, compact option-chain rows,
ITM-side shading and emphasized strike column. Standard MCP host font and color
variables override the Kite fallbacks. The build reuses the embedded Open Sans
font from the auth templates and includes its SIL OFL notice in the generated
bundle; it does not load a font from the network.

Large-chain screenshots cover light/dark desktop and mobile. Browser checks
also verify custom host palette/font overrides, tablet account-total sizing,
ITM rendering and existing widget behavior.

Remaining component review: funds cards, mobile instrument rows, inspector tabs,
quote/depth metrics, historical charts, order statuses/timelines, GTT legs,
empty/loading/error states, and portfolio chart disclosures now share the Kite
spacing and control treatments. Removed empty chart readout space and corrected
late-inserted chart style specificity. Candle canvas text uses the effective host
font and resolves theme-aware CSS colors through computed styles before drawing.
Account widget captures include mobile dark-mode views and overflow assertions.
All four browser suites and JavaScript unit tests pass after this review.

Final review corrected cancellation recovery for active auxiliary widgets, shared
quote request pacing (main and auxiliary calls), and dark semantic host color
overrides. Regression tests cover cancellation/retry/late responses, paced quote
starts with cancelled queued work, and host-provided success colors.

Model-visible view entrypoint: `open_kite_view` declares model/app visibility,
links the shared resource, and delegates strict read-only view selections.
Browser checks cover direct holdings, positions, funds, orders, GTT and chain
launches using original view arguments and returned origin metadata. SDK tests
cover discovery, source exclusions, mutation rejection, and transport invocation.
Server instructions, ordinary tool descriptions and relevant workflow help now
explain when to use the view tool, with ordinary-tool fallback for non-Apps clients.
