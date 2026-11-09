---
title: Order Approvals
description: Review orders in your client or browser and manage future order permissions
---

# Order Approvals

When you ask Kite MCP to place, modify, or cancel an order, it asks you to review
the action unless you have already allowed future orders for this connection.
Clients with approval forms show the details in your conversation. Other clients
return a link so you can approve in your browser.

No order is submitted while the request is waiting for approval. Saying “yes”
in chat does not replace the approval form or browser review.

## Review in your browser

1. Ask your assistant to prepare the order and resolve any missing details.
2. When you want to submit it, request the placement, modification, or cancellation.
3. Open the approval link in any browser and sign in to the matching Kite account.
4. Check the account, requesting client, instrument, exchange, direction,
   quantity, product, order type, price, and validity.
5. Select **Place this order**, **Apply these changes**, or **Cancel this order**
   if the details match your intention. Otherwise, decline or close the page.
6. Return to your assistant and ask it to check the approval's outcome. Do not
   repeat the original order request.

Your assistant selects the approval method from the client's capabilities.
On ChatGPT mobile, it should choose browser review automatically and share the
link. If the approval form cannot be shown, your assistant should use browser
review. You do not need to request a link separately.

An explicit decline, cancellation, or negative confirmation stops the request.
The assistant must not start another review unless you ask it to.

Links expire after five minutes. Approval permits one submission; it does not
guarantee exchange execution. Cancelling an order does not reverse existing fills.

## Approve an exact order for later

For a new order, select **Approve for later. Do not place now** to
save a one-time approval without sending an order to Kite. This is separate
from the checkbox that allows any future order. Saving cannot enable that
broader permission.

The assistant must wait for a new instruction to place the order. A later
request from the same client and Kite login can use the saved approval only
when every reviewed order detail matches. Changed details need a new approval
unless you have already allowed this client to place any future order without
asking. Saving an exact-order approval does not turn off that wider permission.
The approval is consumed by one submission attempt, including a failed or
uncertain attempt. It expires with the login session or server restart and can
be revoked in order approval settings. This does not schedule an order or
monitor the market for a condition.

`approval_status` returns `approved_for_later` while the approval is unused,
with its expiry and a link to revoke it. This status means no order was placed.
`permissions` reports `saved_order_approvals` separately from
`future_orders_allowed`.

## Allow future orders from this client

The browser page has an optional **Let this client place, modify, and cancel
orders without asking** checkbox, with its expiry shown in the label. It is
unchecked by default. If permission is already active, the page shows its scope
and revocation link instead of another checkbox.

Leaving it unchecked approves only the displayed action. If the submission
succeeds, checking it also lets that client place, modify, and cancel orders for
this account without individual approval, until the displayed expiry, logout,
or revocation. If the request fails, the result page confirms that future orders still require approval. Instructions the
client follows unintentionally can also lead to orders while this is enabled.

This permission applies to this client's current Kite MCP login session. It does
not transfer to another client or a new login. It does not grant GTT or Alerts/ATO
permission; those tools have their own behaviour described below.

## Check or turn off future-order permission

Ask your assistant whether future orders are currently allowed. It can check
without placing an order or changing your permission.

If permission is active, select **Ask me before every order** on the result page,
or ask your assistant:

> Show me the link to require approval again for future orders from this client.

Open the link, sign in to Kite, and select **Ask me before every order**. Future requests
will need approval. Revoking permission does not cancel orders already submitted.

## Choose a client

| Client | Approval options |
|--------|------------------|
| ChatGPT web | Review the form in your conversation, or request a browser approval link. |
| ChatGPT mobile | The assistant selects browser review automatically and shares the link. |
| Pi and other code mode clients | Open the browser link shared by your assistant. |
| Other clients with form support | Review the native form, or request browser approval. |

Browser approval does not require your client to display MCP forms or interactive
charts. You can open the link on another device or in another browser. Each
browser review requires a fresh Kite sign-in to the account that requested the
order. Sign-in does not submit the order; you still need to confirm the details.
Kite MCP does not save an approval cookie or bind reviews to a browser.

## If something goes wrong

| What you see | What to do |
|--------------|------------|
| Expired link | Check that no submission occurred, then request a new review if you still want the action. |
| Declined approval | The order was not submitted. Leave it unapproved unless you decide to request it again. |
| Order details changed | Request a new review with the current order details. |
| Kite error or unclear result after approval | Check the order book and fills before requesting another submission. |
| Browser approval unavailable | Reconnect the client. If the server still does not offer browser approvals, use a client with native approval forms. |

Do not automatically retry an uncertain submission.

## Which actions use this approval?

The approval and optional future permission cover placement, modification, and
cancellation through `orders`. Reading orders, trades, holdings, or market data
does not require this approval.

GTT and Alerts/ATO changes are outside this flow and can create future trades.
Request those changes only when you intend them. [[Code Mode]] explains how
assistants should handle pending approvals during analysis.
