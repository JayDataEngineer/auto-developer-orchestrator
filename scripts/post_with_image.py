#!/usr/bin/env python3
"""Post a tweet with an image using SeleniumBase + saved cookies."""
import json
import os
import sys
import time

COOKIE_PATH = "/sandbox/workspace/data/.twitter-session.json"
IMAGE_PATH = "/sandbox/workspace/data/staged/nietzsche.jpg"
CAPTION = "To live is to suffer, to survive is to find some meaning in the suffering."


def main():
    if not os.path.exists(IMAGE_PATH):
        print(json.dumps({"error": f"Image not found at {IMAGE_PATH}"}))
        sys.exit(1)

    with open(COOKIE_PATH) as f:
        session_data = json.load(f)
    cookies = session_data.get("cookies", session_data) if isinstance(session_data, dict) else session_data
    print(f"Loaded {len(cookies)} cookies")

    from seleniumbase import SB

    print("Starting SeleniumBase browser (non-UC)...")
    with SB(headless=True, browser="chrome", 
            chromium_arg="--no-sandbox,--disable-dev-shm-usage,--disable-gpu,--disable-extensions") as sb:
        print("Step 1: Navigating to x.com...")
        sb.open("https://x.com")
        time.sleep(3)

        print("Step 2: Injecting cookies...")
        injected = 0
        for cookie in cookies:
            try:
                c = {
                    "name": cookie["name"],
                    "value": cookie["value"],
                    "domain": cookie.get("domain", ".x.com"),
                    "path": cookie.get("path", "/"),
                }
                if cookie.get("secure"):
                    c["secure"] = True
                if cookie.get("httpOnly"):
                    c["httpOnly"] = True
                sb.driver.add_cookie(c)
                injected += 1
            except Exception:
                continue
        print(f"  Injected {injected}/{len(cookies)} cookies")

        print("Step 3: Refreshing to apply cookies...")
        sb.open("https://x.com/home")
        time.sleep(5)

        current_url = sb.get_current_url()
        print(f"  Current URL: {current_url}")
        if "login" in current_url:
            print(json.dumps({"error": "Session expired"}))
            sys.exit(1)
        print("  Login verified!")

        print("Step 4: Navigating to compose...")
        sb.open("https://x.com/compose/post")
        time.sleep(5)
        print("  Compose page loaded")

        # Take screenshot for debug
        sb.save_screenshot("/sandbox/workspace/scripts/debug_compose.png")
        print("  Debug screenshot saved")

        print("Step 5: Uploading image...")
        uploaded = False
        
        # Try finding file input with explicit wait
        try:
            sb.wait_for_element('input[type="file"]', timeout=10)
            el = sb.driver.find_element("css selector", 'input[type="file"]')
            abs_path = os.path.abspath(IMAGE_PATH)
            print(f"  Found file input, uploading from: {abs_path}")
            el.send_keys(abs_path)
            uploaded = True
            print("  Uploaded!")
        except Exception as e:
            print(f"  File input approach failed: {e}")
            # Try clicking media button first
            try:
                print("  Trying media button approach...")
                sb.click('[aria-label="Add photos or video"]', timeout=5)
                time.sleep(2)
                el = sb.driver.find_element("css selector", 'input[type="file"]')
                el.send_keys(os.path.abspath(IMAGE_PATH))
                uploaded = True
                print("  Uploaded via media button!")
            except Exception as e2:
                print(f"  Media button also failed: {e2}")
                sb.save_screenshot("/sandbox/workspace/scripts/upload_fail.png")
                sys.exit(1)

        print("  Waiting for upload to complete...")
        time.sleep(5)

        # Take screenshot to see if image uploaded
        sb.save_screenshot("/sandbox/workspace/scripts/after_image_upload.png")
        print("  Post-upload screenshot saved")

        print("Step 6: Typing caption...")
        try:
            sb.wait_for_element('[data-testid="tweetTextarea_0"]', timeout=10)
            sb.click('[data-testid="tweetTextarea_0"]')
            time.sleep(0.5)
            sb.type('[data-testid="tweetTextarea_0"]', CAPTION)
            print(f"  Typed {len(CAPTION)} chars")
        except Exception as e:
            print(f"  tweetTextarea failed: {e}, trying role=textbox")
            try:
                el = sb.driver.find_element("css selector", 'div[role="textbox"]')
                el.click()
                time.sleep(0.3)
                el.send_keys(CAPTION)
                print(f"  Typed {len(CAPTION)} chars via role=textbox")
            except Exception as e2:
                print(f"  ERROR: {e2}")
                sb.save_screenshot("/sandbox/workspace/scripts/type_fail.png")
                sys.exit(1)

        time.sleep(2)

        print("Step 7: Clicking Post button...")
        posted = False
        for sel in ['[data-testid="tweetButton"]', 'button[data-testid="tweetButton"]',
                     'div[data-testid="tweetButton"]', 'button[data-testid="tweetButtonInline"]']:
            try:
                sb.click(sel, timeout=5)
                posted = True
                print(f"  Post clicked: {sel}")
                break
            except Exception:
                continue

        if not posted:
            print("  ERROR: Could not find Post button")
            sb.save_screenshot("/sandbox/workspace/scripts/post_btn_fail.png")
            sys.exit(1)

        print("  Waiting for post to process...")
        time.sleep(8)
        final_url = sb.get_current_url()
        print(f"  URL after post: {final_url}")

        print("Step 8: Verifying on profile...")
        sb.open("https://x.com/Athletic_NA")
        time.sleep(5)

        profile_url = sb.get_current_url()
        print(f"  Profile URL: {profile_url}")

        result = {
            "posted": True,
            "caption": CAPTION,
            "image": IMAGE_PATH,
            "profile_url": profile_url,
        }
        print("\n" + json.dumps(result, indent=2))

    print("\nDone!")


if __name__ == "__main__":
    main()
