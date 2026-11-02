# Kite workflow help
Start with a topic, then follow a recipe key. `help({"key":"/"})` lists topics; `help({"key":"/orders"})` lists order recipes; `help({"key":"/orders/reconcile"})` reads one recipe. Trailing slashes are accepted. Keys are exact, case-sensitive paths; wildcards and filesystem paths are not supported.

These workflows use the server's actual tools. Check `tools/list` for tools enabled in this deployment. Help reads embedded documentation; it never fetches account data or executes trades. Examples are illustrative and must be adapted to the user's request. Transaction approval always remains with the server and client.

For analyses using generated tool calls, read `help({"key":"/client-integration/code-mode"})` for result handling, pagination, and approval rules.

When an order returns `status=approval_required`, share its browser link and wait for user review. Do not open or approve it for the user or repeat the order call. Check the outcome with `orders` mode=`approval_status` using `approval_id`. Future order permission can be granted only by the user on that browser page; use `orders` mode=`permissions` to get its revocation link when enabled.
