# Caddy Configuration for SSE Proxying

This Caddyfile configuration demonstrates how to properly proxy Server-Sent Events (SSE) through Caddy to your backend application.

## Configuration

```
example.com {
    # matchers
    @sse   path /sse*
    @nosse not path /sse*

    # gzip everything except /sse endpoints
    encode @nosse gzip

    # SSE proxy: no buffering, no timeouts, keep connection alive
    reverse_proxy @sse your-backend-host:port {
        # disable Caddy's proxy buffering
        flush_interval -1

        # ensure the EventSource handshake stays live
        header_up Connection    keep-alive
        header_up Cache-Control no-cache

        # turn off all upstream timeouts
        transport http {
            response_header_timeout 0
            read_timeout            0
            write_timeout           0
        }
    }

    # fallback for all other requests
    reverse_proxy your-backend-host:port
}
```

## Explanation

This configuration:

1. Creates two matchers:
   - `@sse`: Matches all paths starting with `/sse`
   - `@nosse`: Matches all paths NOT starting with `/sse`

2. Enables gzip compression for all non-SSE endpoints

3. Sets up a special reverse proxy configuration for SSE endpoints:
   - Disables buffering with `flush_interval -1`
   - Sets appropriate headers to keep the connection alive
   - Disables timeouts to prevent the SSE connection from being terminated

4. Provides a fallback proxy for all other requests

## Usage

1. Replace `example.com` with your domain
2. Replace `your-backend-host:port` with your actual backend service address
3. Save this configuration to your Caddyfile
4. Reload Caddy with `caddy reload`

## Important Notes

- SSE connections are long-lived and require special handling to prevent timeouts
- Disabling compression for SSE endpoints is important to ensure real-time delivery
- The configuration assumes your backend is properly implementing SSE responses
