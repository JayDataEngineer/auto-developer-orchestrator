#!/usr/bin/env python3
"""SeleniumBase stealth browser helper for the agent.

Called via bash from the orchestrator agent. Outputs JSON to stdout.
Uses SeleniumBase Pure CDP Mode (sb_cdp) — no WebDriver, fully stealthy.

Usage:
    sb_agent.py navigate <url> [--stealth]
    sb_agent.py search <query> [--stealth]
    sb_agent.py extract_images <url> [--stealth]
    sb_agent.py interact <url> [--stealth]
    sb_agent.py screenshot <url> <output_path> [--stealth]
    sb_agent.py download <url> <output_path>
    sb_agent.py run <python_script> [--stealth]

Commands:
    navigate <url>            Navigate to URL, return page text + image URLs + links
    search <query>            Search Google, return top results as text + links
    extract_images <url>      Navigate and return ONLY image URLs (fastest for image hunting)
    interact <url>            Navigate and return interactive elements (buttons, inputs, links)
    screenshot <url> <path>   Navigate, take screenshot, save to path
    download <url> <path>     Download a file to the given path
    run <python_code>         Execute Python code with `sb` (SeleniumBase CDP) pre-initialized.
                              The browser starts on about:blank. Use sb.get(url) to navigate.
                              Available: sb.get(), sb.click(), sb.type(), sb.select_all(),
                              sb.find_element(), sb.get_text(), sb.get_title(), sb.get_current_url(),
                              sb.sleep(), sb.solve_captcha(), sb.go_back(), sb.scroll_down(),
                              sb.save_screenshot(), sb.evaluate() and all sb_cdp methods.
                              Return JSON by setting `result` dict variable.
                              If no `result` variable, page data is returned automatically.

Flags:
    --stealth             Use UC Mode (SB with uc=True) for maximum anti-bot bypass
"""

import sys
import json
import os

# Suppress SeleniumBase startup noise
os.environ["SB_NO_BORING_RC"] = "1"

MAX_TEXT = 4000
MAX_IMAGES = 50
MAX_LINKS = 30
MAX_ELEMENTS = 50
TIMEOUT = 30


def output(data):
    """Print JSON to stdout and exit."""
    print(json.dumps(data, ensure_ascii=False))
    sys.exit(0)


def output_error(msg):
    """Print error JSON and exit."""
    print(json.dumps({"error": str(msg)}, ensure_ascii=False))
    sys.exit(1)


def safe(fn, default=None):
    """Call fn and return default on any exception."""
    try:
        return fn()
    except Exception:
        return default


def extract_data(sb):
    """Extract page data from a SeleniumBase instance."""
    title = safe(lambda: sb.get_title() or "")
    url = safe(lambda: sb.get_current_url() or "")
    text = safe(lambda: sb.get_text("body") or "")
    if text and len(text) > MAX_TEXT:
        text = text[:MAX_TEXT] + "...[truncated]"

    images = []
    try:
        for img in sb.select_all("img[src]", timeout=3):
            src = img.get_attribute("src") or ""
            if src and not src.startswith("data:") and len(src) < 2000:
                images.append(src)
                if len(images) >= MAX_IMAGES:
                    break
    except Exception:
        pass

    links = []
    try:
        for a in sb.select_all("a[href]", timeout=3):
            href = a.get_attribute("href") or ""
            if href and not href.startswith("#") and not href.startswith("javascript:"):
                link_text = (a.text or "").strip()[:100]
                links.append({"text": link_text, "url": href})
                if len(links) >= MAX_LINKS:
                    break
    except Exception:
        pass

    return {
        "title": title,
        "url": url,
        "text": text,
        "images": images,
        "links": links,
    }


def extract_interactive(sb):
    """Extract interactive elements (links, buttons, inputs) with CSS selectors."""
    elements = []

    # Links
    try:
        for a in sb.select_all("a[href]", timeout=3):
            href = a.get_attribute("href") or ""
            text = (a.text or "").strip()[:80]
            if href and not href.startswith("#") and not href.startswith("javascript:"):
                elements.append({
                    "type": "link",
                    "text": text,
                    "selector": f"a[href='{href}']" if len(href) < 200 else "a",
                    "url": href,
                })
                if len(elements) >= MAX_ELEMENTS:
                    break
    except Exception:
        pass

    # Buttons
    try:
        for btn in sb.select_all("button, input[type='submit'], input[type='button'], [role='button']", timeout=2):
            text = (btn.text or btn.get_attribute("value") or "").strip()[:80]
            if text:
                elements.append({"type": "button", "text": text})
                if len(elements) >= MAX_ELEMENTS:
                    break
    except Exception:
        pass

    # Inputs
    try:
        for inp in sb.select_all("input:not([type='hidden']):not([type='submit']):not([type='button']), textarea, select", timeout=2):
            name = inp.get_attribute("name") or inp.get_attribute("id") or inp.get_attribute("placeholder") or ""
            itype = inp.get_attribute("type") or inp.tag_name
            if name:
                elements.append({"type": "input", "input_type": itype, "name": name})
                if len(elements) >= MAX_ELEMENTS:
                    break
    except Exception:
        pass

    return elements


def with_browser(fn, url=None, stealth=False):
    """Create a browser, run fn, then close it."""
    if stealth:
        from seleniumbase import SB
        with SB(uc=True, test=True, locale="en", xvfb=True) as sb:
            if url:
                sb.activate_cdp_mode(url)
                sb.sleep(2)
            else:
                sb.activate_cdp_mode("about:blank")
            return fn(sb)
    else:
        from seleniumbase import sb_cdp
        start_url = url or "about:blank"
        sb = sb_cdp.Chrome(start_url, xvfb=True)
        try:
            if url:
                sb.sleep(2)
            return fn(sb)
        finally:
            try:
                sb.driver.stop()
            except Exception:
                pass


def cmd_navigate(url, stealth=False):
    """Navigate to a URL and extract page data."""
    data = with_browser(lambda sb: extract_data(sb), url=url, stealth=stealth)
    output(data)


def cmd_search(query, stealth=False):
    """Search Google and return results."""
    url = f"https://www.google.com/search?q={query.replace(' ', '+')}&num=10"
    data = with_browser(lambda sb: extract_data(sb), url=url, stealth=stealth)
    output(data)


def cmd_extract_images(url, stealth=False):
    """Navigate and return only image URLs."""
    def extract(sb):
        images = []
        try:
            for img in sb.select_all("img[src]", timeout=5):
                src = img.get_attribute("src") or ""
                if src and not src.startswith("data:") and len(src) < 2000:
                    alt = img.get_attribute("alt") or ""
                    images.append({"src": src, "alt": alt})
                    if len(images) >= MAX_IMAGES:
                        break
        except Exception:
            pass
        return {"url": url, "images": images}

    data = with_browser(extract, url=url, stealth=stealth)
    output(data)


def cmd_interact(url, stealth=False):
    """Navigate and return interactive elements."""
    def extract(sb):
        data = extract_data(sb)
        data["elements"] = extract_interactive(sb)
        return data

    data = with_browser(extract, url=url, stealth=stealth)
    output(data)


def cmd_screenshot(url, path, stealth=False):
    """Navigate, take screenshot, save to path."""
    def take(sb):
        sb.save_screenshot(path)
        data = extract_data(sb)
        data["screenshot_path"] = path
        return data

    data = with_browser(take, url=url, stealth=stealth)
    output(data)


def cmd_download(url, output_path):
    """Download a file."""
    import urllib.request
    try:
        urllib.request.urlretrieve(url, output_path)
        size = os.path.getsize(output_path)
        output({"url": url, "path": output_path, "size": size, "success": True})
    except Exception as e:
        output_error(f"Download failed: {e}")


def cmd_run(code, stealth=False):
    """Execute Python code with a pre-initialized SeleniumBase browser.

    The `sb` variable is the SeleniumBase CDP instance.
    If the code sets a `result` dict variable, it's returned as JSON.
    Otherwise, page data is extracted and returned.
    """
    def execute(sb):
        # Create execution namespace with sb pre-loaded
        namespace = {
            "sb": sb,
            "json": json,
            "os": os,
        }
        try:
            exec(code, namespace)
        except Exception as e:
            return {"error": str(e), "output": None}

        # If code set a `result` variable, return it
        if "result" in namespace and isinstance(namespace["result"], dict):
            return namespace["result"]

        # Otherwise return current page data
        return extract_data(sb)

    data = with_browser(execute, url=None, stealth=stealth)
    output(data)


def main():
    args = sys.argv[1:]
    if not args:
        print(__doc__)
        sys.exit(1)

    command = args[0]
    stealth = "--stealth" in args
    # Filter out flags
    positional = [a for a in args[1:] if not a.startswith("--")]

    try:
        if command == "navigate":
            if not positional:
                output_error("Usage: sb_agent.py navigate <url>")
            cmd_navigate(positional[0], stealth)

        elif command == "search":
            if not positional:
                output_error("Usage: sb_agent.py search <query>")
            cmd_search(positional[0], stealth)

        elif command == "extract_images":
            if not positional:
                output_error("Usage: sb_agent.py extract_images <url>")
            cmd_extract_images(positional[0], stealth)

        elif command == "interact":
            if not positional:
                output_error("Usage: sb_agent.py interact <url>")
            cmd_interact(positional[0], stealth)

        elif command == "screenshot":
            if len(positional) < 2:
                output_error("Usage: sb_agent.py screenshot <url> <output_path>")
            cmd_screenshot(positional[0], positional[1], stealth)

        elif command == "download":
            if len(positional) < 2:
                output_error("Usage: sb_agent.py download <url> <output_path>")
            cmd_download(positional[0], positional[1])

        elif command == "run":
            if not positional:
                output_error("Usage: sb_agent.py run <python_code>")
            cmd_run(positional[0], stealth)

        else:
            output_error(f"Unknown command: {command}. Use navigate, search, extract_images, interact, screenshot, download, or run.")

    except Exception as e:
        output_error(f"{command} failed: {e}")


if __name__ == "__main__":
    main()
