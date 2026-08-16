# SOCIAL_SENTIMENT

Reddit + X + StockTwits sentiment patterns via web MCP. This skill is baked into the **news-analyst** role prompt.

## Why Social Matters
Social sentiment is a **contrarian signal at extremes** and a **momentum confirmation at moderate levels**:
- **Extreme bullish sentiment** (everyone long): often a top
- **Extreme bearish sentiment** (capitulation): often a bottom
- **Moderate bullish momentum** (rising chatter, increasing mentions): trend confirmation

## Reddit Patterns

### Subreddit targeting
- `/r/wallstreetbets` — high-volatility retail traders, meme stocks, options flow
- `/r/stocks` — more sober fundamental discussion
- `/r/investing` — long-term, Boglehead-leaning
- `/r/StockMarket` — mixed
- `/r/cryptocurrency` — crypto general
- `/r/Bitcoin`, `/r/ethereum`, `/r/solana` — coin-specific
- `/r/RealDaytrading` — actual day traders
- `/r/algotrading` — quant/systematic

### Site-restricted searches
```python
# WallStreetBets mentions
web_research_research(
    query="site:reddit.com/r/wallstreetbets $SYMBOL",
    max_results=5
)

# Specific coin or stock discussion
web_research_research(
    query="site:reddit.com/r/cryptocurrency $COIN sentiment",
    max_results=5
)

# Recent threads (last 24h)
web_research_search(
    query="site:reddit.com/r/wallstreetbets $SYMBOL",
    time_filter="day",
    top_k=10
)
```

### Reading Reddit threads
```python
web_research_fetch(url="https://www.reddit.com/r/wallstreetbets/comments/{thread_id}/")
```

For Reddit specifically, append `.json` to get raw JSON:
```python
web_research_fetch(url="https://www.reddit.com/r/wallstreetbets/comments/{thread_id}.json?limit=100")
```

## X / Twitter Patterns

X has the fastest signal for breaking news. Use search operators:
```python
# Stock-specific chatter
web_research_search(query="$SYMBOL (stock OR earnings) (bullish OR bearish)", top_k=10)

# Crypto-specific chatter
web_research_search(query="$COIN (BTC OR ETH OR crypto) (long OR short)", top_k=10)

# Follow specific accounts
web_research_search(query="from:unusual_whales $SYMBOL", top_k=5)
web_research_search(query="from:zerohedge $SYMBOL", top_k=5)

# Cashtags ($AAPL)
web_research_search(query="\\$AAPL (from:realWillMeade OR from:DanZanger)", top_k=5)
```

**Verified accounts to follow** (high signal-to-noise):
- `@unusual_whales` — options flow
- `@WatcherGuru` — crypto news
- `@Eurostoxx50` — macro
- `@zerohedge` — bearish macro (contrarian indicator)
- `@chamath` — venture / growth
- `@CathieDArk` — innovation

## StockTwits Patterns

```python
web_research_fetch(url="https://stocktwits.com/symbol/$SYMBOL")
```

StockTwits shows a "bullish/bearish" sentiment ratio from users. Extreme readings (>80% bullish or >80% bearish) are contrarian signals.

## Sentiment Aggregation

For each asset, combine the social signals:

```python
def social_sentiment_score(mentions, sentiment_ratio, momentum):
    """
    mentions: count of mentions in last 24h vs 7d avg (e.g., 3x normal)
    sentiment_ratio: bullish / (bullish + bearish), 0-1
    momentum: change in mentions last 7d (e.g., +50%)
    """
    # Volume surge = attention (can be bullish or bearish)
    volume_factor = min(mentions / 3, 2.0)  # cap at 2x

    # Sentiment score (-1 to +1)
    sentiment = (sentiment_ratio - 0.5) * 2

    # Extreme sentiment = contrarian
    if abs(sentiment) > 0.7:
        contrarian_factor = -0.3 * (abs(sentiment) - 0.7) / 0.3  # flip partial signal
    else:
        contrarian_factor = 0

    return sentiment + contrarian_factor, volume_factor
```

## Meme Stock Detection
WallStreetBets + extreme volume + high short interest = potential gamma squeeze.

```python
web_research_research(
    query="$SYMBOL short interest float wallstreetbets gamma squeeze",
    max_results=3
)
```

Flags:
- Short interest > 20% of float
- WSB mentions > 1000/day
- Stock up > 50% in 5 days
- Catalyst: short sellers covering, gamma squeeze on calls

**Warning**: Meme stocks can spike 100%+ then crash 80%. Don't get caught holding.

## Common Patterns

### Pump-and-dump signatures
- Coordinated posts across Reddit, X, Discord at the same time
- Low-float microcap (under $50M market cap)
- "To the moon" language, diamond hands emojis
- Press releases from "interview" sites (not Tier 1)

### Capitulation signatures (bullish contrarian)
- WSB "it's over" posts at peak volume
- High-quality coins down 70%+ with no fundamental change
- " capitulation" language in mainstream media

## Reddit-Specific Rules
- **DD (Due Diligence) posts**: long-form fundamental analysis. Higher signal than memes.
- **Loss porn**: reverse indicator. People posting huge losses often mark bottoms.
- **YOLO posts**: people posting all-in bets. Reverse indicator for short-term.

## Source Quality
- Reddit `/r/stocks` DD posts > WSB memes
- WSB "DD" tag > WSB "YOLO" tag
- X verified accounts with real names > anonymous accounts
- CoinTelegraph > random crypto blogs

## Output Schema

```json
{
  "symbol": "GME",
  "asset_class": "stock",
  "social_score": -0.25,
  "confidence": 0.55,
  "metrics": {
    "mentions_24h": 1247,
    "mentions_7d_avg": 380,
    "volume_factor": 3.28,
    "bullish_pct": 0.78,
    "bearish_pct": 0.22,
    "sentiment_trend_7d": "rising"
  },
  "subreddit_breakdown": {
    "wallstreetbets": {"score": 0.65, "extreme": true, "contrarian_flag": true},
    "stocks": {"score": -0.10, "extreme": false},
    "ShortSqueeze": {"score": 0.80, "extreme": true, "contrarian_flag": true}
  },
  "key_threads": [
    {"sub": "/r/wallstreetbets", "title": "GME to the moon 🚀🚀🚀", "upvotes": 4500, "url": "..."}
  ],
  "extreme_reading": true,
  "contrarian_signal": "bearish_extreme_bullishness"
}
```

## Pitfalls
- **Bot networks**: coordinated X accounts pushing a narrative. Check follower counts + age.
- **Pump groups**: Discord/Telegram groups explicitly coordinate pumps. Don't fall for it.
- **Reposts**: same content from multiple "influencers" = astroturfed.
- **Emotional language**: "to the moon", "diamond hands", "apes together strong" = peak euphoria, contrarian sell signal.
