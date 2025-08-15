#!/usr/bin/env node

const readline = require('readline');
const https = require('https');
const { URL } = require('url');

class KiteExtensionProxy {
  constructor() {
    this.config = {
      serverUrl: 'https://mcp.kite.trade/mcp',
      timeoutSeconds: 30,
      retryAttempts: 3
    };
    this.sessionId = null; // Store the session ID
    this.setupMCPServer();
  }

  validateRequest(request) {
    if (!request || typeof request !== 'object') {
      throw new Error('Request must be a valid object');
    }

    if (!request.jsonrpc || request.jsonrpc !== '2.0') {
      throw new Error('Invalid JSON-RPC version');
    }

    if (!request.method || typeof request.method !== 'string') {
      throw new Error('Method must be a non-empty string');
    }

    if (request.id !== undefined && typeof request.id !== 'string' && typeof request.id !== 'number') {
      throw new Error('ID must be a string or number');
    }

    // Validate method names against allowed list
    const allowedMethods = [
      'initialize', 'tools/list', 'tools/call', 'notifications/tools/list_changed'
    ];
    
    if (!allowedMethods.includes(request.method)) {
      throw new Error(`Method '${request.method}' is not allowed`);
    }

    // Validate tools/call specific parameters
    if (request.method === 'tools/call') {
      if (!request.params || typeof request.params !== 'object') {
        throw new Error('tools/call requires params object');
      }
      
      if (!request.params.name || typeof request.params.name !== 'string') {
        throw new Error('tools/call requires valid tool name');
      }

      // Validate tool names against available tools
      const availableTools = this.getTools().map(tool => tool.name);
      if (!availableTools.includes(request.params.name)) {
        throw new Error(`Tool '${request.params.name}' is not available`);
      }

      // Validate tool arguments against schema
      const toolSchema = this.getTools().find(tool => tool.name === request.params.name)?.inputSchema;
      if (toolSchema && request.params.arguments) {
        this.validateToolArguments(request.params.arguments, toolSchema);
      }

      // Sanitize tool arguments
      if (request.params.arguments) {
        this.sanitizeArguments(request.params.arguments);
      }
    }

    return true;
  }

  validateToolArguments(args, schema) {
    if (!schema || !schema.properties) {
      return; // No schema to validate against
    }

    // Check required fields
    if (schema.required) {
      for (const requiredField of schema.required) {
        if (!(requiredField in args)) {
          throw new Error(`Missing required field: ${requiredField}`);
        }
      }
    }

    // Validate field types and constraints
    for (const [field, value] of Object.entries(args)) {
      const fieldSchema = schema.properties[field];
      if (!fieldSchema) {
        continue; // Allow additional properties for flexibility
      }

      // Type validation
      if (fieldSchema.type) {
        const expectedType = fieldSchema.type;
        const actualType = Array.isArray(value) ? 'array' : typeof value;
        
        if (actualType !== expectedType) {
          throw new Error(`Field '${field}' must be of type ${expectedType}, got ${actualType}`);
        }

        // Enum validation
        if (fieldSchema.enum && !fieldSchema.enum.includes(value)) {
          throw new Error(`Field '${field}' must be one of: ${fieldSchema.enum.join(', ')}`);
        }

        // Array items validation
        if (expectedType === 'array' && fieldSchema.items) {
          for (let i = 0; i < value.length; i++) {
            const item = value[i];
            const itemType = typeof item;
            if (itemType !== fieldSchema.items.type) {
              throw new Error(`Array '${field}' item ${i} must be of type ${fieldSchema.items.type}, got ${itemType}`);
            }
          }
        }
      }
    }
  }

  sanitizeArguments(args) {
    if (!args || typeof args !== 'object') {
      return;
    }

    // Remove any potentially dangerous properties
    delete args.__proto__;
    delete args.constructor;
    delete args.prototype;

    // Recursively sanitize nested objects
    for (const key in args) {
      if (args.hasOwnProperty(key)) {
        const value = args[key];
        
        // Sanitize strings to prevent injection
        if (typeof value === 'string') {
          // Remove null bytes and control characters except newlines/tabs
          args[key] = value.replace(/[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g, '');
        } else if (typeof value === 'object' && value !== null) {
          this.sanitizeArguments(value);
        }
      }
    }
  }

  getTools() {
    return [
      {
        name: 'login',
        description: 'Login to Kite API. This tool helps you log in to the Kite API. If you are starting off a new conversation call this tool before hand. Call this if you get a session error. Returns a link that the user should click to authorize access.',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'get_holdings',
        description: 'Get your current stock holdings from your demat account',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'get_positions',
        description: 'Get your current open positions (intraday and carry forward)',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'get_margins',
        description: 'Get available margins and funds for trading',
        inputSchema: {
          type: 'object',
          properties: {
            segment: {
              type: 'string',
              description: 'Trading segment (equity or commodity)',
              enum: ['equity', 'commodity']
            }
          }
        }
      },
      {
        name: 'get_orders',
        description: 'Get list of all orders for the day',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'get_trades',
        description: 'Get list of all executed trades for the day',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'get_quotes',
        description: 'Get real-time market quotes for instruments',
        inputSchema: {
          type: 'object',
          properties: {
            instruments: {
              type: 'array',
              description: 'List of instruments in format EXCHANGE:TRADINGSYMBOL (e.g., NSE:RELIANCE)',
              items: {
                type: 'string'
              }
            }
          },
          required: ['instruments']
        }
      },
      {
        name: 'search_instruments',
        description: 'Search for trading instruments by name or symbol',
        inputSchema: {
          type: 'object',
          properties: {
            query: {
              type: 'string',
              description: 'Search query (symbol or company name)'
            },
            exchange: {
              type: 'string',
              description: 'Exchange to search in',
              enum: ['NSE', 'BSE', 'NFO', 'CDS', 'BFO', 'MCX']
            }
          },
          required: ['query']
        }
      },
      {
        name: 'get_historical_data',
        description: 'Get historical price data for an instrument',
        inputSchema: {
          type: 'object',
          properties: {
            instrument_token: {
              type: 'string',
              description: 'Instrument token'
            },
            from_date: {
              type: 'string',
              description: 'Start date (YYYY-MM-DD)'
            },
            to_date: {
              type: 'string',
              description: 'End date (YYYY-MM-DD)'
            },
            interval: {
              type: 'string',
              description: 'Candle interval',
              enum: ['minute', '3minute', '5minute', '10minute', '15minute', '30minute', '60minute', 'day']
            }
          },
          required: ['instrument_token', 'from_date', 'to_date', 'interval']
        }
      },
      {
        name: 'get_profile',
        description: 'Get user profile information',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'place_order',
        description: 'Place a new order',
        inputSchema: {
          type: 'object',
          properties: {
            exchange: {
              type: 'string',
              description: 'Exchange'
            },
            tradingsymbol: {
              type: 'string',
              description: 'Trading symbol'
            },
            transaction_type: {
              type: 'string',
              description: 'BUY or SELL',
              enum: ['BUY', 'SELL']
            },
            quantity: {
              type: 'number',
              description: 'Quantity to trade'
            },
            order_type: {
              type: 'string',
              description: 'Order type',
              enum: ['MARKET', 'LIMIT', 'SL', 'SL-M']
            },
            product: {
              type: 'string',
              description: 'Product type',
              enum: ['CNC', 'NRML', 'MIS']
            },
            price: {
              type: 'number',
              description: 'Price for LIMIT orders'
            },
            trigger_price: {
              type: 'number',
              description: 'Trigger price for SL orders'
            }
          },
          required: ['exchange', 'tradingsymbol', 'transaction_type', 'quantity', 'order_type', 'product']
        }
      },
      {
        name: 'modify_order',
        description: 'Modify an existing order',
        inputSchema: {
          type: 'object',
          properties: {
            order_id: {
              type: 'string',
              description: 'Order ID to modify'
            },
            quantity: {
              type: 'number',
              description: 'New quantity'
            },
            price: {
              type: 'number',
              description: 'New price'
            },
            trigger_price: {
              type: 'number',
              description: 'New trigger price'
            },
            order_type: {
              type: 'string',
              description: 'New order type',
              enum: ['MARKET', 'LIMIT', 'SL', 'SL-M']
            }
          },
          required: ['order_id']
        }
      },
      {
        name: 'cancel_order',
        description: 'Cancel an existing order',
        inputSchema: {
          type: 'object',
          properties: {
            order_id: {
              type: 'string',
              description: 'Order ID to cancel'
            }
          },
          required: ['order_id']
        }
      },
      {
        name: 'get_order_history',
        description: 'Get detailed execution history for a specific order',
        inputSchema: {
          type: 'object',
          properties: {
            order_id: {
              type: 'string',
              description: 'Order ID to get history for'
            }
          },
          required: ['order_id']
        }
      },
      {
        name: 'get_order_trades',
        description: 'Get all trades executed for a specific order',
        inputSchema: {
          type: 'object',
          properties: {
            order_id: {
              type: 'string',
              description: 'Order ID to get trades for'
            }
          },
          required: ['order_id']
        }
      },
      {
        name: 'get_gtts',
        description: 'Get list of Good Till Triggered (GTT) orders',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'get_mf_holdings',
        description: 'Get mutual fund holdings and investment details',
        inputSchema: {
          type: 'object',
          properties: {}
        }
      },
      {
        name: 'get_ltp',
        description: 'Get last traded price for specified instruments',
        inputSchema: {
          type: 'object',
          properties: {
            instruments: {
              type: 'array',
              description: 'List of instruments in format EXCHANGE:TRADINGSYMBOL',
              items: {
                type: 'string'
              }
            }
          },
          required: ['instruments']
        }
      },
      {
        name: 'get_ohlc',
        description: 'Get OHLC (Open, High, Low, Close) data for instruments',
        inputSchema: {
          type: 'object',
          properties: {
            instruments: {
              type: 'array',
              description: 'List of instruments in format EXCHANGE:TRADINGSYMBOL',
              items: {
                type: 'string'
              }
            }
          },
          required: ['instruments']
        }
      },
      {
        name: 'place_gtt_order',
        description: 'Create Good Till Triggered orders for automated trading',
        inputSchema: {
          type: 'object',
          properties: {
            trigger_type: {
              type: 'string',
              description: 'GTT trigger type',
              enum: ['single', 'two-leg']
            },
            tradingsymbol: {
              type: 'string',
              description: 'Trading symbol'
            },
            exchange: {
              type: 'string',
              description: 'Exchange'
            },
            trigger_values: {
              type: 'array',
              description: 'Trigger price values',
              items: {
                type: 'number'
              }
            },
            last_price: {
              type: 'number',
              description: 'Last traded price'
            },
            orders: {
              type: 'array',
              description: 'Order details',
              items: {
                type: 'object'
              }
            }
          },
          required: ['trigger_type', 'tradingsymbol', 'exchange', 'trigger_values', 'last_price', 'orders']
        }
      },
      {
        name: 'modify_gtt_order',
        description: 'Modify existing GTT orders',
        inputSchema: {
          type: 'object',
          properties: {
            trigger_id: {
              type: 'string',
              description: 'GTT trigger ID to modify'
            },
            trigger_type: {
              type: 'string',
              description: 'GTT trigger type',
              enum: ['single', 'two-leg']
            },
            tradingsymbol: {
              type: 'string',
              description: 'Trading symbol'
            },
            exchange: {
              type: 'string',
              description: 'Exchange'
            },
            trigger_values: {
              type: 'array',
              description: 'Trigger price values',
              items: {
                type: 'number'
              }
            },
            last_price: {
              type: 'number',
              description: 'Last traded price'
            },
            orders: {
              type: 'array',
              description: 'Order details',
              items: {
                type: 'object'
              }
            }
          },
          required: ['trigger_id', 'trigger_type', 'tradingsymbol', 'exchange', 'trigger_values', 'last_price', 'orders']
        }
      },
      {
        name: 'delete_gtt_order',
        description: 'Delete Good Till Triggered orders',
        inputSchema: {
          type: 'object',
          properties: {
            trigger_id: {
              type: 'string',
              description: 'GTT trigger ID to delete'
            }
          },
          required: ['trigger_id']
        }
      }
    ];
  }

  setupMCPServer() {
    const rl = readline.createInterface({
      input: process.stdin,
      output: null,
      terminal: false
    });

    rl.on('line', async (line) => {
      if (!line.trim()) return;
      
      try {
        const request = JSON.parse(line);
        const response = await this.handleMCPRequest(request);
        process.stdout.write(JSON.stringify(response) + '\n');
      } catch (error) {
        const errorResponse = {
          jsonrpc: '2.0',
          id: null,
          error: {
            code: -32700,
            message: error.message
          }
        };
        process.stdout.write(JSON.stringify(errorResponse) + '\n');
      }
    });

    rl.on('close', () => {
      process.exit(0);
    });
  }

  async makeHttpRequest(data, attempt = 1) {
    return new Promise(async (resolve, reject) => {
      // Add exponential backoff delay for retries
      if (attempt > 1) {
        const delay = Math.min(1000 * Math.pow(2, attempt - 2), 10000); // Cap at 10 seconds
        console.error(`Retrying request (attempt ${attempt}/${this.config.retryAttempts}) after ${delay}ms delay`);
        await new Promise(delayResolve => setTimeout(delayResolve, delay));
      }
      const url = new URL(this.config.serverUrl);
      const postData = JSON.stringify(data);
      
      const headers = {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(postData),
        'User-Agent': 'KiteExtensionProxy/1.0.0'
      };

      // Include session ID header if we have one
      if (this.sessionId) {
        headers['Mcp-Session-Id'] = this.sessionId;
      }


      const options = {
        hostname: url.hostname,
        port: url.port || 443,
        path: url.pathname,
        method: 'POST',
        headers,
        timeout: this.config.timeoutSeconds * 1000
      };

      const req = https.request(options, (res) => {
        let responseData = '';
        
        res.on('data', (chunk) => {
          responseData += chunk;
        });
        
        res.on('end', () => {
          try {
            if (res.statusCode >= 200 && res.statusCode < 300) {
              const response = JSON.parse(responseData);
              
              // Extract session ID from response headers
              const sessionId = res.headers['mcp-session-id'];
              if (sessionId && !this.sessionId) {
                this.sessionId = sessionId;
              }
              
              resolve(response);
            } else {
              reject(new Error(`HTTP ${res.statusCode}: ${responseData}`));
            }
          } catch (error) {
            reject(new Error(`Invalid JSON response: ${responseData}`));
          }
        });
      });

      req.on('error', async (error) => {
        console.error(`Request error on attempt ${attempt}:`, {
          message: error.message,
          code: error.code,
          timestamp: new Date().toISOString()
        });
        
        // Reset session ID on session-related errors
        if (error.message && error.message.includes('session')) {
          this.sessionId = null;
          console.error('Session error detected, clearing session ID');
        }
        
        if (attempt < this.config.retryAttempts) {
          try {
            const result = await this.makeHttpRequest(data, attempt + 1);
            resolve(result);
          } catch (retryError) {
            console.error('All retry attempts failed:', {
              attempts: attempt,
              maxAttempts: this.config.retryAttempts,
              finalError: retryError.message
            });
            reject(retryError);
          }
        } else {
          console.error('Request failed after all attempts:', {
            attempts: this.config.retryAttempts,
            error: error.message
          });
          reject(error);
        }
      });

      req.on('timeout', () => {
        req.destroy();
        reject(new Error('Request timeout'));
      });

      req.write(postData);
      req.end();
    });
  }

  async handleMCPRequest(request) {
    try {
      // Validate and sanitize the request
      this.validateRequest(request);
      
      const { method, params, id } = request;
      let result;

      switch (method) {
        case 'initialize':
          // Forward initialize request to server to get session ID
          return await this.makeHttpRequest(request);
          break;

        case 'tools/list':
          result = {
            tools: this.getTools()
          };
          break;

        case 'tools/call':
          const { name, arguments: args } = params;
          if (name === 'login') {
            // Forward login request to server to get OAuth URL
            return await this.makeHttpRequest(request);
          } else {
            // For all other tools, forward to server
            return await this.makeHttpRequest(request);
          }
          break;

        default:
          return await this.makeHttpRequest(request);
      }

      return {
        jsonrpc: '2.0',
        id,
        result
      };
    } catch (error) {
      return {
        jsonrpc: '2.0',
        id,
        error: {
          code: -32603,
          message: error.message
        }
      };
    }
  }
}

// Handle errors with proper logging
process.on('uncaughtException', (error) => {
  console.error('Uncaught Exception:', {
    message: error.message,
    stack: error.stack,
    timestamp: new Date().toISOString()
  });
  
  // Attempt graceful shutdown
  try {
    // Give some time for pending operations
    setTimeout(() => {
      process.exit(1);
    }, 1000);
  } catch (shutdownError) {
    console.error('Error during shutdown:', shutdownError.message);
    process.exit(1);
  }
});

process.on('unhandledRejection', (reason, promise) => {
  console.error('Unhandled Promise Rejection:', {
    reason: reason instanceof Error ? {
      message: reason.message,
      stack: reason.stack
    } : reason,
    promise: promise.toString(),
    timestamp: new Date().toISOString()
  });
  
  // For unhandled rejections, we might not want to exit immediately
  // depending on the severity, but for safety we'll exit
  setTimeout(() => {
    process.exit(1);
  }, 1000);
});

// Start the extension
new KiteExtensionProxy();