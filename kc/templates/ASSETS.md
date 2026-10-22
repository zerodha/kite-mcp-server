# Login page assets

`brand_assets.html` contains unmodified official Kite and Zerodha SVG logos from
https://kite.zerodha.com/static/images/kite-logo.svg and
https://kite.zerodha.com/static/images/zerodha-logo.svg, captured 2026-10-23.
These remain Zerodha brand assets.

The embedded Open Sans Latin variable WOFF2 comes from the official login page's
Google Fonts stylesheet (https://fonts.googleapis.com/css?family=Open+Sans:300,400,600).
It is licensed under the SIL Open Font License; see `OpenSans-OFL.txt`.
The font and logos use data URLs so auth pages make no third-party asset requests
and require no new public asset route. Only Latin is needed for current page text.
