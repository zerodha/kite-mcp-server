# Kite workflow help
Start with a topic, then follow a recipe key. `help({"key":"/"})` lists topics; `help({"key":"/orders"})` lists order recipes; `help({"key":"/orders/reconcile"})` reads one recipe. Trailing slashes are accepted. Keys are exact, case-sensitive paths; wildcards and filesystem paths are not supported.

These workflows use the server's actual tools. Check `tools/list` for tools enabled in this deployment. Help reads embedded documentation; it never fetches account data or executes trades. Examples are illustrative and must be adapted to the user's request. Transaction approval always remains with the server and client.
