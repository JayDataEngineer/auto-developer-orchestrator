#!/usr/bin/env python3
"""historical_research.py — Research plan generator for historical walk dates.

The historical walk in backtest_scan.md needs QUALITATIVE context per date,
not just numerical data. The news-analyst and filings-analyst specialists
already know HOW to call web MCP tools — what they lack is WHICH URLs and
queries to use for a specific past date.

This script generates that plan. Output is a JSON structure the agent can
iterate, calling web MCP tools for each entry. Cache via alt_data.py ensures
we don't re-fetch the same URL twice in one walk.

Usage:
    python3 sandbox/historical_research.py --date 2026-04-01
    python3 sandbox/historical_research.py --date 2026-04-01 --ticker AAPL
    python3 sandbox/historical_research.py --date 2026-04-01 --output data/research_plan.json

Output shape:
    {
      "date": "2026-04-01",
      "window": {"start": "2026-03-25", "end": "2026-04-01"},
      "tickers": [
        {
          "symbol": "AAPL",
          "cik": "0000320193",
          "research_queries": ["Apple Q2 2026 earnings", "AAPL news April 2026"],
          "scrape_urls": [
            "https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=...&type=8-K&dateb=20260401",
            "https://finance.yahoo.com/quote/AAPL/news/"
          ],
          "freshest_filing_type": "8-K"
        }
      ],
      "macro_context": {
        "fred_observations": ["FEDFUNDS", "DGS10", "DGS2", "CPIAUCSL"],
        "research_queries": ["Fed minutes April 2026", "CPI report March 2026"]
      }
    }
"""
import argparse
import json
import os
import sys
from datetime import datetime, timedelta

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths


# CIK mapping for SEC EDGAR. Extend as watchlist grows.
# Source: https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&type=10-K
TICKER_CIK = {
    "AAPL": "0000320193",
    "MSFT": "0000789019",
    "GOOGL": "0001652044",
    "GOOG": "0001652044",
    "NVDA": "0001045810",
    "META": "0001326801",
    "TSLA": "0001318605",
    "AMZN": "0001018724",
    "NFLX": "0001065280",
    "AMD": "0000002488",
    "INTC": "0000050863",
    "COIN": "0001679788",
}

DEFAULT_STOCKS = ["AAPL", "MSFT", "GOOGL", "NVDA", "META", "TSLA", "AMZN"]
DEFAULT_CRYPTO = ["BTC/USD", "ETH/USD", "SOL/USD"]
DEFAULT_FRED = ["FEDFUNDS", "DGS10", "DGS2", "CPIAUCSL", "UNRATE"]


def edgar_filings_url(cik: str, filing_type: str, dateb: str, datad: str = "") -> str:
    """Build SEC EDGAR browse URL with date filter.

    dateb = filings BEFORE this date (YYYYMMDD)
    datad = filings AFTER this date (YYYYMMDD) — optional window start
    """
    base = (
        "https://www.sec.gov/cgi-bin/browse-edgar"
        f"?action=getcompany&CIK={cik}&type={filing_type}"
        f"&dateb={dateb}"
    )
    if datad:
        base += f"&datad={datad}"
    return base + "&action=getcompany"


def yahoo_news_url(symbol: str) -> str:
    return f"https://finance.yahoo.com/quote/{symbol}/news/"


def yahoo_history_url(symbol: str) -> str:
    return f"https://finance.yahoo.com/quote/{symbol}/history?p={symbol}"


def coingecko_url(coin_id: str, date: str) -> str:
    """CoinGecko historical snapshot URL (date format: dd-mm-yyyy)."""
    dt = datetime.strptime(date, "%Y-%m-%d")
    formatted = dt.strftime("%d-%m-%Y")
    return f"https://www.coingecko.com/en/coins/{coin_id}/historical_data?start_date={formatted}&end_date={formatted}"


def crypto_to_coingecko(symbol: str) -> str:
    """BTC/USD -> bitcoin, ETH/USD -> ethereum."""
    base = symbol.split("/")[0].upper()
    return {
        "BTC": "bitcoin",
        "ETH": "ethereum",
        "SOL": "solana",
        "XRP": "ripple",
        "ADA": "cardano",
        "DOGE": "dogecoin",
    }.get(base, base.lower())


def fred_url(series_id: str) -> str:
    return f"https://fred.stlouisfed.org/series/{series_id}"


def generate_plan(date: str, tickers=None, crypto=None, fred_series=None, window_days: int = 7) -> dict:
    """Build the research plan for a given historical date."""
    dt = datetime.strptime(date, "%Y-%m-%d")
    window_start = dt - timedelta(days=window_days)
    dateb = dt.strftime("%Y%m%d")
    datad = window_start.strftime("%Y%m%d")
    month_year = dt.strftime("%B %Y")

    stocks = tickers or DEFAULT_STOCKS
    cryptos = crypto or DEFAULT_CRYPTO
    fred = fred_series or DEFAULT_FRED

    ticker_plans = []
    for sym in stocks:
        cik = TICKER_CIK.get(sym, "")
        research_queries = [
            f"{sym} earnings {month_year}",
            f"{sym} stock news {month_year}",
            f"{sym} analyst ratings {dt.strftime('%Y-%m')}",
        ]

        scrape_urls = [
            yahoo_news_url(sym),
            yahoo_history_url(sym),
        ]
        if cik:
            scrape_urls.insert(0, edgar_filings_url(cik, "8-K", dateb, datad))
            scrape_urls.append(edgar_filings_url(cik, "10-Q", dateb, datad))

        ticker_plans.append({
            "symbol": sym,
            "cik": cik,
            "asset_class": "stock",
            "research_queries": research_queries,
            "scrape_urls": scrape_urls,
            "priority_filings": ["8-K"] + (["10-Q"] if dt.month in (3, 6, 9, 12) else []),
        })

    for sym in cryptos:
        coin_id = crypto_to_coingecko(sym)
        ticker_plans.append({
            "symbol": sym,
            "asset_class": "crypto",
            "research_queries": [
                f"{sym} news {month_year}",
                f"{sym.replace('/', ' ')} funding rate {dt.strftime('%Y-%m')}",
                f"{coin_id} on-chain metrics {month_year}",
            ],
            "scrape_urls": [
                coingecko_url(coin_id, date),
                f"https://www.coinglass.com/FundingRate",
            ],
        })

    return {
        "date": date,
        "window": {
            "start": window_start.strftime("%Y-%m-%d"),
            "end": date,
            "days": window_days,
        },
        "tickers": ticker_plans,
        "macro_context": {
            "fred_observations": fred,
            "scrape_urls": [fred_url(s) for s in fred],
            "research_queries": [
                f"Fed minutes {month_year}",
                f"CPI report {(dt - timedelta(days=15)).strftime('%B %Y')}",
                f"treasury yields {dt.strftime('%Y-%m')}",
                f"labor market {(dt - timedelta(days=15)).strftime('%B %Y')}",
            ],
        },
        "agent_instructions": (
            "For each ticker: call research(query) for the top query, then scrape(urls) for "
            "the URLs that look most promising based on the research result. Use alt_data.py "
            "cache-store to save each result so the next walk date can cache-lookup instead of "
            "re-fetching. Skip any URL that returns 404 or paywall — note in the report."
        ),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--date", required=True, help="Historical date (YYYY-MM-DD)")
    parser.add_argument("--ticker", help="Limit to single ticker")
    parser.add_argument("--window-days", type=int, default=7, help="Look-back window (default 7)")
    parser.add_argument("--output", help="Write plan to file (default: stdout)")
    args = parser.parse_args()

    try:
        datetime.strptime(args.date, "%Y-%m-%d")
    except ValueError:
        print(f"ERROR: --date must be YYYY-MM-DD, got {args.date!r}", file=sys.stderr)
        sys.exit(2)

    if args.ticker:
        plan = generate_plan(args.date, tickers=[args.ticker], crypto=[], fred_series=[])
    else:
        plan = generate_plan(args.date, window_days=args.window_days)

    if args.output:
        os.makedirs(os.path.dirname(args.output) or ".", exist_ok=True)
        with open(args.output, "w") as f:
            json.dump(plan, f, indent=2)
        print(f"Plan written: {args.output}")
        print(f"  {len(plan['tickers'])} tickers, {len(plan['macro_context']['research_queries'])} macro queries")
    else:
        print(json.dumps(plan, indent=2))


if __name__ == "__main__":
    main()
