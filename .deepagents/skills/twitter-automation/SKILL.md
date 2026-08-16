---
name: twitter-automation
description: Drive x.com via the cookie session + SeleniumBase — read timeline, draft and post, engage with mentions. Includes the captcha-solving fallback (LLM-via-vision) for Arkose / hCaptcha / Turnstile. Use when the Twitter agent hits a captcha wall or needs to verify a login challenge actually cleared.
---

# Captcha Solving — LLM-via-Vision

When browser automation hits a captcha (Twitter Arkose funcaptcha, Cloudflare Turnstile, hCaptcha, etc.), solve it through your existing vision tool — no paid service.

## Detection

Signs you've hit a captcha wall:
- Browser landed on a URL containing `arkose`, `captcha`, `funcaptcha`, or `interstitial`
- The page text contains "Verify your account", "Are you a human?", or "Please complete the puzzle"
- An iframe from `arkoselabs.com`, `hcaptcha.com`, `challenges.cloudflare.com`
- After posting/reading, the response is a 429 with `x-rate-limit-reason: captcha_required`

## Solve flow

```
1. Screenshot the captcha widget region (full-page screenshot is fine)
2. Call vision via mcp_call:
     server: media-analysis
     tool: analyze_image
     args:
       imageSource: <data URI of the screenshot>
       prompt: "This is a captcha challenge. Identify the concrete action
                to solve it: what to click, rotate, drag, or type.
                Return JSON: {action, target, coordinates?}"
3. Apply the suggested action via browser tools
4. Wait 2 seconds. Screenshot again. If new challenge appears, repeat.
5. Max 5 attempts. After that, save state, sync_session, abort with reason.
```

## Per-captcha-type prompts

The vision model needs task-specific prompts. Use these:

### Twitter Arkose funcaptcha (3D rotation / tile match)
```
"This is an Arkose funcaptcha. The challenge is to <rotate the 3D object
until it matches the silhouette / pick the tile that matches the symbol>.
Identify the EXACT action: how many degrees to rotate, or which tile
coordinates to click. Return JSON: {action, amount?, tile_index?}"
```

### hCaptcha (pick N tiles matching X)
```
"This is an hCaptcha grid. The instruction is at the top: '<pick all
traffic lights>'. Identify which tile indices (0-8, left-to-right
top-to-bottom) match. Return JSON: {tiles: [0,3,5]}"
```

### Cloudflare Turnstile
```
"Cloudflare Turnstile. Usually a single checkbox click. Return JSON:
{action: 'click', target: 'checkbox'}"
```

### Text captcha
```
"What characters are shown in the distorted text image? Return JSON:
{text: '<the text>'}"
```

## Honesty rules

- **Don't claim success without verification.** After applying the action, take another screenshot and confirm the challenge actually went away (URL changed, new content loaded).
- **If the captcha refreshes**, the new challenge may be easier or harder. Try twice, then abort.
- **Never brute force.** Five distinct attempts max, not five clicks on the same target.

## When to give up

After 5 failed attempts on the same captcha:
1. Save the screenshot to `/sandbox/workspace/captcha_failed_<timestamp>.png`
2. Run `sync_session` to refresh cookies (cookie may have stale arkose token)
3. If a fresh session still hits the captcha, report: `"captcha_unsolvable", challenge_type, screenshot_path`
4. The CTO can escalate to a human

## Avoidance first

Captcha solving is a fallback. Prefer:
- **Cookie auth** via `sync_session` — sidesteps login captchas entirely
- **Rate limit respect** — back off on 429s before they escalate to captchas
- **Realistic timing** — 5+ second delays between actions reduce suspicion
