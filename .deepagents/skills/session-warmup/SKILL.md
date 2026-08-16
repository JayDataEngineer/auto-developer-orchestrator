---
name: session-warmup
description: >-
  Build fingerprint legitimacy by visiting benign high-traffic sites before
  sensitive targets. Call browser_warmup_history once at session start for
  auth-heavy sites (LinkedIn login, Workday applications, Twitter posting) so
  the browser looks like a real user, not fresh automation. Skip for general
  browsing — it burns 15-30s.
---

# Session Warmup — Build Fingerprint Legitimacy ONCE

For sensitive targets (LinkedIn login, Workday applications, Twitter posting),
call **`browser_warmup_history` ONCE at the start of the session** before
navigating to the target.

## What it does

Visits benign high-traffic sites (Wikipedia, Hacker News, GitHub, Stack
Overflow) with realistic dwell times + scroll, so the browser's history +
cookie jar + TLS fingerprint look like a real user rather than a fresh
automation session that went straight `about:blank → target`.

## When to use

- **Use**: Before navigating to a site with aggressive anti-bot detection
  (LinkedIn, Workday, Twitter, job portals with Cloudflare/Turnstile).
- **Skip**: For general browsing (Wikipedia, news sites, public pages). It
  burns ~15-30 seconds of dwell time for no benefit on sites that don't
  fingerprint.

## Combats

"Fresh automation" heuristics — anti-bot services flag browsers whose history
jar shows no prior activity. A warmup round makes the first `navigate` to the
target look like the Nth site visited, not the 1st.

Don't overuse — one warmup per session is enough; multiple rounds don't add
legitimacy and waste time.
