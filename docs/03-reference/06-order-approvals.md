---
title: Order Approvals
description: Review orders in your client or browser and manage future order permissions
---

# Order Approvals

When you ask Kite MCP to place, modify, or cancel an order, it asks you to review
the action. Clients with approval forms show the details in your conversation.
Other clients return a link so you can approve in your browser.

No order is submitted while the request is waiting for approval. Saying “yes”
in chat does not replace the approval form or browser review.

## Review in your browser

1. Ask your assistant to prepare the order and resolve any missing details.
2. When you want to submit it, request the placement, modification, or cancellation.
3. Open the approval link in the browser you used to sign in to Kite MCP.
4. Check the account, requesting client, instrument, exchange, direction,
   quantity, product, order type, price, and validity.
5. Select **Place this order**, **Apply these changes**, or **Cancel this order**
   if the details match your intention. Otherwise, decline or close the page.
6. Return to your assistant and ask it to check the approval's outcome. Do not
   repeat the original order request.

Your assistant selects the approval method from the client's capabilities.
On ChatGPT mobile, it should choose browser review automatically and share the
link. If a native form failure reaches the server, it returns browser review when
available. A client that fails locally before replying cannot trigger that
fallback, so known mobile clients should choose browser review up front.
You do not need to request a link separately.

An explicit decline, cancellation, or negative confirmation stops the request.
The assistant must not start another review unless you ask it to.

Links expire after two minutes. Approval permits one submission; it does not
guarantee exchange execution. Cancelling an order does not reverse existing fills.

## Allow future orders from this client

The browser page has an optional **Allow future orders from this client without
asking again** checkbox. It is unchecked by default.

Leaving it unchecked approves only the displayed action. If the submission
succeeds, checking it also lets that client place, modify, and cancel orders for this account without individual
approval, until the displayed expiry, logout, or revocation. If the request fails, the
result page confirms that future orders still require approval. Instructions the
client follows unintentionally can also lead to orders while this is enabled.

This permission applies to this client's current Kite MCP login session. It does
not transfer to another client or a new login. It does not grant GTT or Alerts/ATO
permission; those tools have their own behaviour described below.

## Require approval again

After allowing future orders, select **Require approval again** on the result
page. You can also ask your assistant:

> Show me the link to require approval again for future orders from this client.

Open the link it returns and select **Require approval again**. Future requests
will need approval. Revoking permission does not cancel orders already submitted.

## Choose a client

| Client | Approval options |
|--------|------------------|
| ChatGPT web | Review the form in your conversation, or request a browser approval link. |
| ChatGPT mobile | The assistant selects browser review automatically and shares the link. |
| Pi and other code mode clients | Open the browser link shared by your assistant. |
| Other clients with form support | Review the native form, or request browser approval. |

Browser approval does not require your client to display MCP forms or interactive
charts. Keep the link private and open it in the browser used for this connection.
If the page asks you to sign in, reconnect your client in that browser and start
a new order review.

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
