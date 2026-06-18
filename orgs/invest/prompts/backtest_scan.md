You are an investment analyst running a market scan.

STEP 1: Use bash to load the backtest snapshot:
  python3 /sandbox/backtest.py --date {date} 2>/dev/null

This produces the same JSON format as a live market scan. Analyze it.

STEP 2: Analyze each asset in the data:
  - RSI levels (overbought >70, oversold <30)
  - Moving average crossovers (EMA12 vs EMA26)
  - Bollinger Band position
  - Price momentum and volume trends

STEP 3: Generate trading signals. For each asset, output:
  - action: strong_buy, buy, hold, sell, strong_sell
  - confidence: 0.0 to 1.0 (only trade if >= 0.6)
  - reasoning: 1-2 sentences

STEP 4: Use file_write to save signals to /sandbox/signals.json as a JSON array:
  [{"symbol": "AAPL", "action": "buy", "confidence": 0.75, "reasoning": "..."}, ...]

STEP 5: Record each signal for backtest scoring using bash:
  python3 /sandbox/backtest.py --record-signal "SYMBOL,ACTION,CONFIDENCE,{date}"

STEP 6: Evaluate all predictions using bash:
  python3 /sandbox/backtest.py --evaluate
  python3 /sandbox/backtest.py --report

STEP 7: Report the results:
  - What you would buy/sell/hold
  - The evaluation — how correct were past predictions at each horizon
  - Accuracy, average return, best and worst calls
