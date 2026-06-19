"""
Crypto on-chain + funding rate + exchange flow client.
Public APIs only — no auth required.

Usage:
    python3 crypto.py                          # Full snapshot (BTC + ETH)
    python3 crypto.py --symbol BTCUSDT         # Single symbol
    python3 crypto.py --funding-only           # Funding rates only
    python3 crypto.py --onchain BTC            # On-chain metrics for BTC
"""

import argparse
import json
import sys
import urllib.request
import urllib.error
from datetime import datetime


BINANCE_FAPI = "https://fapi.binance.com/fapi/v1"
BINANCE_API = "https://api.binance.com/api/v3"
BLOCKCHAIN_INFO = "https://blockchain.info"


def fetch_funding_rate(symbol="BTCUSDT"):
    """Binance futures funding rate (8h)."""
    url = f"{BINANCE_FAPI}/premiumIndex?symbol={symbol}"
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
        rate_8h = float(data.get("lastFundingRate", 0))
        return {
            "symbol": symbol,
            "mark_price": float(data.get("markPrice", 0)),
            "funding_rate_8h": rate_8h,
            "funding_rate_annualized": round(rate_8h * 3 * 365, 6),
            "next_funding_time": data.get("nextFundingTime"),
            "timestamp": data.get("time"),
        }
    except Exception as e:
        return {"symbol": symbol, "error": str(e)}


def fetch_open_interest(symbol="BTCUSDT"):
    """Binance open interest (USD notional)."""
    url = f"{BINANCE_FAPI}/openInterest?symbol={symbol}"
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
        return {"symbol": symbol, "open_interest_contracts": float(data.get("openInterest", 0))}
    except Exception as e:
        return {"symbol": symbol, "error": str(e)}


def fetch_order_book_depth(symbol="BTCUSDT", limit=20):
    """Binance order book top N levels."""
    url = f"{BINANCE_API}/depth?symbol={symbol}&limit={limit}"
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
        bids = data.get("bids", [])[:5]
        asks = data.get("asks", [])[:5]
        bid_wall = sum(float(b[1]) for b in bids)
        ask_wall = sum(float(a[1]) for a in asks)
        return {
            "symbol": symbol,
            "top_bids": [[float(b[0]), float(b[1])] for b in bids],
            "top_asks": [[float(a[0]), float(a[1])] for a in asks],
            "bid_wall_size": bid_wall,
            "ask_wall_size": ask_wall,
            "imbalance": round((bid_wall - ask_wall) / max(bid_wall + ask_wall, 0.0001), 3),
        }
    except Exception as e:
        return {"symbol": symbol, "error": str(e)}


def fetch_btc_onchain():
    """Bitcoin blockchain stats from blockchain.info."""
    endpoints = {
        "difficulty": f"{BLOCKCHAIN_INFO}/q/getdifficulty",
        "block_count": f"{BLOCKCHAIN_INFO}/q/getblockcount",
        "hash_rate": f"{BLOCKCHAIN_INFO}/q/hashrate",
        "total_btc": f"{BLOCKCHAIN_INFO}/q/totalbc",
        "minutes_between_blocks": f"{BLOCKCHAIN_INFO}/q/interval",
    }
    out = {}
    for key, url in endpoints.items():
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "Invest-Research/1.0"})
            with urllib.request.urlopen(req, timeout=10) as resp:
                txt = resp.read().decode("utf-8").strip()
                try:
                    out[key] = float(txt)
                except ValueError:
                    out[key] = txt
        except Exception as e:
            out[key] = {"error": str(e)[:100]}
    return out


def fetch_eth_gas():
    """Etherscan gas oracle (no key required for this public endpoint fallback)."""
    # Public Ethereum RPC gas price
    try:
        payload = json.dumps({
            "jsonrpc": "2.0",
            "method": "eth_gasPrice",
            "params": [],
            "id": 1,
        }).encode()
        req = urllib.request.Request(
            "https://eth.llamarpc.com",
            data=payload,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
        wei = int(data.get("result", "0x0"), 16)
        gwei = wei / 1e9
        return {"gas_price_gwei": round(gwei, 2), "gas_price_wei": wei}
    except Exception as e:
        return {"error": str(e)[:100]}


def composite_crypto_score(symbol, metrics):
    """Compute a composite score from -1.0 (bearish) to +1.0 (bullish).

    Inputs:
        - funding_rate_8h
        - open_interest_trend (caller must compute; here we just use level)
        - bid/ask imbalance
    """
    score = 0
    reasons = []

    funding = metrics.get("funding", {}).get("funding_rate_8h")
    if funding is not None:
        if funding < -0.0001:  # shorts paying longs
            score += 0.20
            reasons.append("negative_funding_bullish")
        elif funding > 0.0005:  # overleveraged longs
            score -= 0.15
            reasons.append("high_funding_long_squeeze_risk")
        elif funding > 0.001:  # extreme
            score -= 0.25
            reasons.append("extreme_funding_squeeze_imminent")

    depth = metrics.get("depth", {})
    if "imbalance" in depth:
        imb = depth["imbalance"]
        if imb > 0.3:
            score += 0.15
            reasons.append("bid_wall_dominant")
        elif imb < -0.3:
            score -= 0.15
            reasons.append("ask_wall_dominant")

    return {
        "symbol": symbol,
        "score": round(max(-1.0, min(1.0, score)), 3),
        "reasons": reasons,
        "confidence": min(1.0, abs(score) * 2),  # higher when score is more extreme
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--symbol", default="BTCUSDT",
                        help="Binance futures symbol (e.g., BTCUSDT, ETHUSDT)")
    parser.add_argument("--funding-only", action="store_true")
    parser.add_argument("--onchain", choices=["BTC", "ETH"], default=None,
                        help="Fetch on-chain metrics")
    args = parser.parse_args()

    if args.onchain:
        if args.onchain == "BTC":
            print(json.dumps({"symbol": "BTC", "onchain": fetch_btc_onchain()}, indent=2))
        else:
            print(json.dumps({"symbol": "ETH", "gas": fetch_eth_gas()}, indent=2))
        return

    if args.funding_only:
        print(json.dumps(fetch_funding_rate(args.symbol), indent=2))
        return

    # Full snapshot
    metrics = {
        "funding": fetch_funding_rate(args.symbol),
        "open_interest": fetch_open_interest(args.symbol),
        "depth": fetch_order_book_depth(args.symbol),
    }

    score = composite_crypto_score(args.symbol, metrics)

    print(json.dumps({
        "timestamp": datetime.now().isoformat(),
        "symbol": args.symbol,
        "metrics": metrics,
        "composite_score": score,
    }, indent=2))


if __name__ == "__main__":
    main()
