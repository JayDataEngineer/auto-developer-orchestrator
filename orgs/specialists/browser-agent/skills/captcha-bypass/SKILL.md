---
name: captcha-bypass
description: >-
  Cloudflare/Turnstile/hCaptcha bypass ladder: browser_solve_captcha (fast JS
  click + honest verify) then browser_uc (real pyautogui click via SB uc=True).
  UC session management for form-filling behind challenges. cf_clearance cookie
  handoff to persistent browser. Use when you see "Just a moment", "Verify you
  are human", or any anti-bot challenge wall.
---

# Captcha & Anti-Bot Bypass — The Ladder

When a page shows a Cloudflare "Just a moment…", "Verify you are human", a
Turnstile/hCaptcha/reCAPTCHA challenge, or any "checking your browser" wall,
climb this ladder in order.

## FAST PATH (saves 3-4 tool calls)

If you recognise the site as Cloudflare/Turnstile-protected (nowsecure.nl,
Workday, Greenhouse, any site that showed a challenge on a previous attempt),
SKIP step 1 and go straight to `browser_uc` with `click_captcha:true`. Don't
waste a `browser_navigate` + `browser_solve_captcha` cycle on a site you KNOW
is caged.

## Step 1 — browser_solve_captcha (fast, persistent browser)

Best-effort JS click + an HONEST verification of whether the challenge is still
on screen. Returns `captcha_solved: true` if the challenge markers are gone,
or `captcha_solved: false` with a `hint` if it can't pass (cross-origin captcha
iframes cannot be clicked via CDP — that's a hard browser limit).

## Step 2 — browser_uc (the real bypass)

If `browser_solve_captcha` returned false OR you recognise a
Turnstile/hCaptcha challenge up front, use `browser_uc` with
`action: "open"`. It spawns a dedicated SeleniumBase `SB(uc=True)` Chrome and
calls `uc_gui_click_captcha` — a **REAL pyautogui mouse click** on the
checkbox, the only reliable way past cross-origin captcha iframes. It then
hands the `cf_clearance` cookie back to the persistent browser so subsequent
`browser_navigate` calls to the same domain inherit the cleared state.

## UC session workflow

Keep the UC session open across click/type/evaluate while you fill the form,
then `action:"close"` when done. Pre-emptive use on known-caged sites (Workday,
some Greenhouse) saves a wasted `browser_navigate` turn.

```
browser_uc {action:"open", url:"https://workday-example.com/job/123", click_captcha:true}
  → cf_cleared: true, cookie_handoff: {injected: 1, names: ["cf_clearance"]}
browser_uc {action:"click", selector:"#apply-button"}
browser_uc {action:"type", selector:"#first-name", text:"Jay"}
browser_uc {action:"type", selector:"#email", text:"jay@example.com", submit:true}
browser_uc {action:"close"}
```

## When to give up

Sites that cage applications behind Turnstile are the #1 reason job-app
automation fails — when in doubt, lead with `browser_uc`. If `browser_uc` with
`click_captcha:true` fails after 2 attempts, the site may have a harder
challenge (Arkose funcaptcha, image grid). Fall back to `web_research` search
to get the information without loading the page.
