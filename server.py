import os
from kiteconnect import KiteConnect
from typing import Any, Optional, List, Dict, Union
from enum import Enum
from pydantic import BaseModel, Field
import httpx
from datetime import datetime
from mcp.server.fastmcp import FastMCP

# ENV VARS
API_KEY = os.getenv("KITE_API_KEY")
API_SECRET = os.getenv("KITE_API_SECRET")

# Initialize FastMCP server
mcp = FastMCP("zerodha-kite")

kite = KiteConnect(api_key=API_KEY, debug=True)

# Enums for order parameters
class OrderVariety(str, Enum):
    REGULAR = "regular"
    CO = "co"
    AMO = "amo"
    ICEBERG = "iceberg"
    AUCTION = "auction"

class Exchange(str, Enum):
    NSE = "NSE"
    BSE = "BSE"
    NFO = "NFO"
    CDS = "CDS"
    BFO = "BFO"
    MCX = "MCX"
    BCD = "BCD"

class TransactionType(str, Enum):
    BUY = "BUY"
    SELL = "SELL"

class ProductType(str, Enum):
    MIS = "MIS"
    CNC = "CNC"
    NRML = "NRML"
    CO = "CO"

class OrderType(str, Enum):
    MARKET = "MARKET"
    LIMIT = "LIMIT"
    SLM = "SL-M"
    SL = "SL"

class Validity(str, Enum):
    DAY = "DAY"
    IOC = "IOC"
    TTL = "TTL"

# Additional Enums
class PositionType(str, Enum):
    DAY = "day"
    OVERNIGHT = "overnight"

class GTTType(str, Enum):
    SINGLE = "single"
    OCO = "two-leg"

class GTTStatus(str, Enum):
    ACTIVE = "active"
    TRIGGERED = "triggered"
    DISABLED = "disabled"
    EXPIRED = "expired"
    CANCELLED = "cancelled"
    REJECTED = "rejected"
    DELETED = "deleted"

class ChartInterval(str, Enum):
    MINUTE = "minute"
    DAY = "day"
    MINUTE_3 = "3minute"
    MINUTE_5 = "5minute"
    MINUTE_10 = "10minute"
    MINUTE_15 = "15minute"
    MINUTE_30 = "30minute"
    MINUTE_60 = "60minute"

# Request model for place_order
class PlaceOrderRequest(BaseModel):
    variety: OrderVariety = Field(..., description="Order variety (regular, co, amo, iceberg, auction)")
    exchange: Exchange = Field(..., description="Trading exchange")
    tradingsymbol: str = Field(..., description="Trading symbol")
    transaction_type: TransactionType = Field(..., description="Transaction type (BUY/SELL)")
    quantity: int = Field(..., description="Order quantity")
    product: ProductType = Field(..., description="Product type")
    order_type: OrderType = Field(..., description="Order type")
    price: Optional[float] = Field(None, description="Order price (required for LIMIT orders)")
    validity: Optional[Validity] = Field(None, description="Order validity")
    validity_ttl: Optional[int] = Field(None, description="Time to live in minutes for TTL validity orders")
    disclosed_quantity: Optional[int] = Field(None, description="Disclosed quantity")
    trigger_price: Optional[float] = Field(None, description="Trigger price for SL orders")
    iceberg_legs: Optional[int] = Field(None, description="Number of legs for iceberg orders")
    iceberg_quantity: Optional[int] = Field(None, description="Quantity per leg for iceberg orders")
    auction_number: Optional[int] = Field(None, description="Auction number for auction orders")
    tag: Optional[str] = Field(None, description="Custom tag for order")

    class Config:
        use_enum_values = True

# Additional Request Models
class ModifyOrderRequest(BaseModel):
    variety: OrderVariety
    order_id: str
    parent_order_id: Optional[str] = None
    quantity: Optional[int] = None
    price: Optional[float] = None
    order_type: Optional[OrderType] = None
    trigger_price: Optional[float] = None
    validity: Optional[Validity] = None
    disclosed_quantity: Optional[int] = None

    class Config:
        use_enum_values = True

class ConvertPositionRequest(BaseModel):
    exchange: Exchange
    tradingsymbol: str
    transaction_type: TransactionType
    position_type: PositionType
    quantity: int
    old_product: ProductType
    new_product: ProductType

    class Config:
        use_enum_values = True

class GTTOrderParams(BaseModel):
    transaction_type: TransactionType
    quantity: int
    order_type: OrderType
    product: ProductType
    price: float

    class Config:
        use_enum_values = True

class PlaceGTTRequest(BaseModel):
    trigger_type: GTTType
    tradingsymbol: str
    exchange: Exchange
    trigger_values: List[float]
    last_price: float
    orders: List[GTTOrderParams]

    class Config:
        use_enum_values = True

class ModifyGTTRequest(PlaceGTTRequest):
    trigger_id: str

    class Config:
        use_enum_values = True

class BasketMarginRequest(BaseModel):
    orders: List[PlaceOrderRequest]
    consider_positions: bool = True
    mode: Optional[str] = None

    class Config:
        use_enum_values = True

# Resource Endpoints
@mcp.tool()
async def get_orders() -> List[Dict]:
    """Get list of orders."""
    return kite.orders()

@mcp.tool()
async def get_trades() -> List[Dict]:
    """Get list of trades."""
    return kite.trades()

@mcp.tool()
async def get_positions() -> Dict:
    """Get user's positions."""
    return kite.positions()

@mcp.tool()
async def get_holdings() -> List[Dict]:
    """Get user's holdings."""
    return kite.holdings()

@mcp.tool()
async def get_margins(segment: Optional[str] = None) -> Dict:
    """Get user's margins."""
    return kite.margins(segment)

@mcp.tool()
async def get_profile() -> Dict:
    """Get user's profile."""
    return kite.profile()

# Tools
@mcp.tool()
async def login_url() -> str:
    """Get login URL for Zerodha KiteConnect."""
    kite.api_key = API_KEY
    return kite.login_url()

@mcp.tool()
async def set_access_token(request_token: str) -> str:
    """Set access token for Zerodha KiteConnect."""
    data = kite.generate_session(request_token, API_SECRET)
    kite.set_access_token(data["access_token"])
    return "Access token set successfully."

@mcp.tool()
async def place_order(request: PlaceOrderRequest) -> str:
    """Place an order on Zerodha Kite platform."""
    order_params = request.dict(exclude_none=True)
    try:
        order_id = kite.place_order(**order_params)
        return f"Order placed successfully. Order ID: {order_id}"
    except Exception as e:
        raise Exception(f"Failed to place order {order_params}: {str(e)}")

@mcp.tool()
async def modify_order(request: ModifyOrderRequest) -> str:
    """Modify an existing order."""
    try:
        order_id = kite.modify_order(
            variety=request.variety,
            order_id=request.order_id,
            **request.dict(exclude={'variety', 'order_id'}, exclude_none=True)
        )
        return f"Order modified successfully. Order ID: {order_id}"
    except Exception as e:
        raise Exception(f"Failed to modify order: {str(e)}")

@mcp.tool()
async def cancel_order(variety: OrderVariety, order_id: str, parent_order_id: Optional[str] = None) -> str:
    """Cancel an order."""
    try:
        order_id = kite.cancel_order(variety, order_id, parent_order_id)
        return f"Order cancelled successfully. Order ID: {order_id}"
    except Exception as e:
        raise Exception(f"Failed to cancel order: {str(e)}")

@mcp.tool()
async def convert_position(request: ConvertPositionRequest) -> str:
    """Convert position's product type."""
    try:
        kite.convert_position(**request.dict())
        return "Position converted successfully."
    except Exception as e:
        raise Exception(f"Failed to convert position: {str(e)}")

@mcp.tool()
async def place_gtt(request: PlaceGTTRequest) -> str:
    """Place a GTT (Good Till Triggered) order."""
    try:
        trigger_id = kite.place_gtt(**request.dict())
        return f"GTT order placed successfully. Trigger ID: {trigger_id}"
    except Exception as e:
        raise Exception(f"Failed to place GTT order: {str(e)}")

@mcp.tool()
async def modify_gtt(request: ModifyGTTRequest) -> str:
    """Modify a GTT (Good Till Triggered) order."""
    try:
        trigger_id = kite.modify_gtt(
            trigger_id=request.trigger_id,
            **request.dict(exclude={'trigger_id'})
        )
        return f"GTT order modified successfully. Trigger ID: {trigger_id}"
    except Exception as e:
        raise Exception(f"Failed to modify GTT order: {str(e)}")

@mcp.tool()
async def delete_gtt(trigger_id: str) -> str:
    """Delete a GTT (Good Till Triggered) order."""
    try:
        kite.delete_gtt(trigger_id)
        return "GTT order deleted successfully."
    except Exception as e:
        raise Exception(f"Failed to delete GTT order: {str(e)}")

@mcp.tool()
async def get_instrument_margins(segment: str) -> Dict:
    """Get margins for instruments."""
    try:
        return kite.margins(segment)
    except Exception as e:
        raise Exception(f"Failed to get instrument margins: {str(e)}")

@mcp.tool()
async def get_basket_margins(request: BasketMarginRequest) -> Dict:
    """Calculate basket margins."""
    try:
        return kite.basket_order_margins(
            [order.dict(exclude_none=True) for order in request.orders],
            consider_positions=request.consider_positions,
            mode=request.mode
        )
    except Exception as e:
        raise Exception(f"Failed to get basket margins: {str(e)}")

@mcp.tool()
async def get_order_history(order_id: str) -> List[Dict]:
    """Get history of an order."""
    try:
        return kite.order_history(order_id)
    except Exception as e:
        raise Exception(f"Failed to get order history: {str(e)}")

@mcp.tool()
async def get_order_trades(order_id: str) -> List[Dict]:
    """Get trades for an order."""
    try:
        return kite.order_trades(order_id)
    except Exception as e:
        raise Exception(f"Failed to get order trades: {str(e)}")

@mcp.tool()
async def get_quote(instruments: List[str]) -> Dict:
    """Get quote for instruments."""
    try:
        return kite.quote(instruments)
    except Exception as e:
        raise Exception(f"Failed to get quote: {str(e)}")

@mcp.tool()
async def get_ohlc(instruments: List[str]) -> Dict:
    """Get OHLC for instruments."""
    try:
        return kite.ohlc(instruments)
    except Exception as e:
        raise Exception(f"Failed to get OHLC: {str(e)}")

@mcp.tool()
async def get_ltp(instruments: List[str]) -> Dict:
    """Get LTP for instruments."""
    try:
        return kite.ltp(instruments)
    except Exception as e:
        raise Exception(f"Failed to get LTP: {str(e)}")

@mcp.tool()
async def get_historical_data(
    instrument_token: int,
    from_date: datetime,
    to_date: datetime,
    interval: ChartInterval,
    continuous: bool = False,
    oi: bool = False
) -> List[Dict]:
    """Get historical data for an instrument."""
    try:
        return kite.historical_data(
            instrument_token,
            from_date,
            to_date,
            interval,
            continuous,
            oi
        )
    except Exception as e:
        raise Exception(f"Failed to get historical data: {str(e)}")

if __name__ == "__main__":
    print("Starting Zerodha Kite MCP server...")
    # TODO: split into two modes sse and stdio.
    mcp.run(transport='stdio')
