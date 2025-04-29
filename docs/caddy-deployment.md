# Caddy Configuration for SSE Proxying

This Caddyfile configuration demonstrates how to properly proxy Server-Sent Events (SSE) through Caddy to your backend application.

## Configuration

Example configuration for SSE-enabled reverse proxy

```
example.com {
    # Define matchers for SSE and non-SSE paths
    @sse   path /sse*
    @nosse not path /sse*

    # Apply gzip compression only to non-SSE endpoints
    encode @nosse gzip

    # Special handling for SSE endpoints with optimized settings
    reverse_proxy @sse backend:port {
        # Disable buffering for immediate event delivery
        flush_interval -1

        # Configure headers for persistent connections in both directions
        header_up Connection    keep-alive
        header_up Cache-Control no-cache
        header_down Connection  keep-alive
        header_down Cache-Control no-cache

        # Disable all timeouts for long-lived SSE connections
        transport http {
            response_header_timeout 0
            read_timeout            0
            write_timeout           0
        }
    }

    # Standard proxy for all other requests
    reverse_proxy backend:port
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
