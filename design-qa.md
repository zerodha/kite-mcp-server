# Kite beta page styling QA

final result: passed

Reference: https://kite.zerodha.com/, captured 2026-10-23 in local Chromium via
Playwright (explicitly authorized after the in-app browser was unavailable).
No old design branch or previous styling implementation was consulted.

## Scope

The shared auth/status templates now use the official login page's measured
visual styles. Authorization and success are active manager templates;
`status.html` remains embedded but is not registered in the manager's template
setup list. The root route continues to serve documentation. No routes changed.
Existing OAuth/app/docs changes were preserved.

## Evidence and comparison

Evidence is in `/tmp/kite-design/`:

- `source-desktop.png` (1440 × 1000), `source-mobile.png` (390 × 844),
  `source-validation.png`, `source.html`, and `details.json`.
- `authorize_interstitial-{1440,390,320}.png`,
  `login_success-{1440,390,320}.png`, and `status-{1440,390,320}.png`.
- `comparison-desktop.png`, `comparison-mobile.png`, and
  `comparison-detail.png` put the source and implementation together.
- `verification.json` records layout, font/image loading, keyboard focus,
  JavaScript errors, and redirect checks.

The official login and MCP consent page contain different content by design.
The comparison evaluates shared branding and visual styles, not identical form
content or identical card heights. No credential form was added.

## Required fidelity surfaces

- Typography: local Open Sans Latin variable WOFF2; 400 regular and 600 emphasis.
  Headings use the measured 21px/31.5px desktop and 24px/36px mobile sizes.
  Body uses 14px desktop and 16px below 1024px. No clipped text was found.
- Layout: 360px desktop card, 60px top margin, 40px padding, 2px #eee border,
  5px radius, and 1px 1px 4px #eee shadow match measured styles. At 550px and
  below, the card fills the width with 40px 30px 20px padding. Logo position
  and heading baseline align in the focused comparison.
- Colors: #f8f8f8 desktop background, white below 1024px, #444 text, and #ff5722
  action match the source. Existing green status text remains a semantic MCP
  element, alongside the retained disclaimer in a neutral bordered box.
- Assets: exact downloaded Kite (60 × 40) and Zerodha (92 × 11) SVGs, embedded
  as data URLs. Font and logos render without external requests. Open Sans
  license and provenance are included under `kc/templates/`.
- Content: all MCP text, disclaimer wording, page titles, version value, and
  dynamic `.KiteLoginURL` remain intact. The footer retains MCP identification.
  Official login's account-signup/store/regulatory links were not added because
  they are outside this existing MCP flow.

## Iteration

Initial comparison found an action height mismatch (1.2 line height versus the
source's 1) and an overly narrow typography/background breakpoint. Corrected to
36.8px action height and the reference's 1023px typography/background breakpoint;
retained its separate 550px card breakpoint. Recaptured implementation and
reviewed desktop detail plus full mobile comparisons. No remaining P0/P1/P2
visual issues. Taller consent card and shorter success/status cards are expected
because existing MCP content is preserved. Footer omits unrelated login links.

## Validation

- `GOCACHE=/tmp/kite-mcp-gocache GOMODCACHE=/tmp/kite-mcp-modcache go test ./...`:
  passed all packages (outside sandbox because httptest needs local listeners).
- All three templates rendered through Go html/template, including embedded
  assets; browser screenshots inspected at 1440, 390, and 320px widths.
- No horizontal overflow, missing images/fonts, or JavaScript errors.
- Tab reaches the consent action; visible focus outline is available.
- Clicking the action preserves the full Kite URL and query. Browser navigation
  was intercepted with a test response; no credentials or real authorization
  were submitted.
- No deployment performed.
