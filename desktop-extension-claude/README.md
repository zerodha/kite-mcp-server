# Kite Connect Desktop Extension

A desktop extension for [Claude Desktop](https://claude.ai/desktop) that provides local access to the Kite Connect trading API through a Model Context Protocol (MCP) server.

## Features

- **Local Execution**: Runs the MCP server locally with your credentials stored securely
- **Cross-Platform**: Works on macOS, Windows, and Linux
- **One-Click Installation**: Install via Claude Desktop's extension system
- **Secure Authentication**: Browser-based authentication flow
- **Fallback Mode**: Automatically falls back to hosted server if local binary fails
- **Complete API Coverage**: Access to all 22 Kite Connect API endpoints

## Installation

### Prerequisites

- [Claude Desktop](https://claude.ai/desktop) version 0.10.0 or later
- Valid Zerodha trading account for OAuth authentication

### Installing the Extension

1. **Get the Extension**: Download the `.dxt` file from the releases page
2. **Install in Claude Desktop**: 
   - Open Claude Desktop
   - Go to Settings > Extensions
   - Click "Install Extension" and select the `.dxt` file
3. **Authenticate**: The extension will guide you through OAuth authentication when first used

## Usage

Once installed and authenticated, you can use natural language to interact with your Kite account:

```
"Show me my current portfolio holdings"
"Get the latest price for RELIANCE"
"What are my open positions?"
"Place a market order to buy 100 shares of TCS"
"Show me my trading history for today"
```

## Available Tools

The extension provides access to all Kite Connect API endpoints:

### Authentication & Setup
- `login` - Login to Kite API and generate authorization link

### Portfolio & Account Management
- `get_profile` - User profile information
- `get_margins` - Available margins and funds
- `get_holdings` - Current stock holdings
- `get_positions` - Open trading positions
- `get_mf_holdings` - Mutual fund holdings

### Market Data
- `get_quotes` - Real-time market quotes
- `get_ltp` - Last traded price
- `get_ohlc` - OHLC data
- `get_historical_data` - Historical price data
- `search_instruments` - Search trading instruments

### Order Management
- `place_order` - Place new orders
- `modify_order` - Modify existing orders
- `cancel_order` - Cancel orders
- `get_orders` - List all orders
- `get_trades` - Trading history
- `get_order_history` - Order execution history
- `get_order_trades` - Trades for specific order

### GTT Orders
- `get_gtts` - List GTT orders
- `place_gtt_order` - Create GTT orders
- `modify_gtt_order` - Modify GTT orders
- `delete_gtt_order` - Delete GTT orders

## Architecture

The extension uses a hybrid architecture:

1. **Primary Mode**: Local Go binary execution for maximum security
2. **Fallback Mode**: Hosted server connection if local binary fails
3. **Node.js Wrapper**: Handles MCP protocol and process management
4. **Secure Storage**: Sessions encrypted in OS keychain

## Security

- **Local Execution**: No data leaves your machine in primary mode
- **Secure Authentication**: Browser-based authentication flow
- **No Stored Credentials**: No API keys or secrets stored locally
- **Session Management**: Secure session handling with automatic token refresh
- **HTTPS Only**: All network communication uses TLS encryption

## Development

### Building from Source

1. **Prerequisites**:
   - Go 1.21 or later
   - Node.js 16 or later
   - DXT CLI: `npm install -g @anthropic-ai/dxt`
   - jq (for version sync): `brew install jq` or `apt-get install jq`

2. **Quick Build** (from repository root):
   ```bash
   # Build extension with automatic version sync
   just build-extension
   
   # Package the extension
   just package-extension
   ```

3. **Manual Build**:
   ```bash
   # Build binaries (includes automatic version sync)
   ./build-binaries.sh
   
   # Package extension
   dxt pack .
   ```

4. **Version Management**:
   ```bash
   # Sync extension version with git tags
   ./sync-version.sh
   ```

5. **Install in Claude Desktop**:
   - Install the generated `.dxt` file

### Testing

To test the extension during development:

1. Build a local binary for your platform
2. Create a symlink in the binaries directory
3. Package and install the extension
4. Test with Claude Desktop

## Troubleshooting

### Common Issues

1. **"Binary not found" error**:
   - Ensure binaries are built for your platform
   - Check that binaries are executable
   - Verify path resolution in the extension

2. **Authentication failures**:
   - Complete the OAuth flow in the browser
   - Ensure your Zerodha account is active
   - Check that you're logging into the correct account

3. **Network issues**:
   - Check firewall settings
   - Verify internet connectivity
   - Try fallback to hosted mode

### Debug Information

The extension provides logging information in the console:

1. Check Claude Desktop's developer console for error messages
2. Look for authentication flow status in the browser
3. Verify that the OAuth redirect completes successfully
4. Check network connectivity if authentication fails

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License - see the [LICENSE](../LICENSE) file for details.

## Support

- **Issues**: Report bugs on [GitHub Issues](https://github.com/zerodha/kite-mcp-server/issues)
- **Documentation**: [Kite Connect API Docs](https://kite.trade/docs/connect/)
- **Community**: Join the discussion on GitHub Discussions

## Acknowledgments

- Built on top of [kite-mcp-server](https://github.com/zerodha/kite-mcp-server)
- Uses [Anthropic's Desktop Extension](https://github.com/anthropics/dxt) specification
- Powered by [Model Context Protocol](https://modelcontextprotocol.io/)