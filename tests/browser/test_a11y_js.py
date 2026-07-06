"""Regression tests for sb_server.py `/a11y` JS.

Background: CDP `Runtime.evaluate` runs the script body RAW — it does not wrap
in a function the way Selenium's `driver.execute_script` does. Top-level
`const`/`function`/`let` declarations therefore leak into the page's
compilation context. Calling `/a11y` twice in the same page throws:

    SyntaxError: Identifier 'out' has already been declared

That broke the entire `find_element` → `click_element` → `mouse_action`
pipeline: the agent fell back to `evaluate_js("...click()")` which doesn't
emit a mouse_action SSE event, so the VNC cursor overlay never rendered.

These tests assert the JS body is wrapped in an IIFE so the bug cannot
silently come back. They run without a browser — pure static analysis of
the source file.
"""
import re
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SB_SERVER = REPO_ROOT / "sandbox" / "scripts" / "sb_server.py"


def extract_a11y_js(source: str) -> str:
    """Pull the JS string passed to sb.execute_script inside the /a11y branch."""
    m = re.search(
        r'path == "/a11y".*?result\s*=\s*sb\.execute_script\(\s*r?(\"\"\"|\'\'\')(.*?)(\1)',
        source,
        re.DOTALL,
    )
    if not m:
        raise AssertionError("could not find /a11y JS in sb_server.py")
    return m.group(2)


_source = SB_SERVER.read_text()
_js = extract_a11y_js(_source)


def test_js_is_iife_wrapped():
    """The JS body must be wrapped in `(function() { ... })()`

    Without an IIFE, `const out = []` and `function buildSelector(el)` leak
    into the page's global scope and the second call throws
    'Identifier ... has already been declared'.
    """
    stripped = _js.strip()
    assert stripped.startswith("(function") or stripped.startswith("(()"), (
        f"/a11y JS must start with an IIFE wrapper, got: {stripped[:60]!r}"
    )
    assert re.search(r"\}\)\s*\(\s*\)\s*$", stripped), (
        "/a11y JS must end with `})()` to invoke the IIFE, got: " + repr(stripped[-60:])
    )


def test_js_has_no_top_level_return_outside_iife():
    """No `return` statements before the IIFE opens.

    Top-level `return` in raw CDP Runtime.evaluate is a SyntaxError
    ("Illegal return statement"). The original broken version was:

        return (() => { ... })()

    which fails on the first call.
    """
    stripped = _js.strip()
    iife_open = stripped.find("(function")
    if iife_open == -1:
        iife_open = stripped.find("(()")
    assert iife_open > -1, "no IIFE wrapper found"
    prefix = stripped[:iife_open]
    # comments are OK, but no `return` as a statement
    # strip // line comments and /* */ block comments
    prefix_no_comments = re.sub(r"//[^\n]*", "", prefix)
    prefix_no_comments = re.sub(r"/\*.*?\*/", "", prefix_no_comments, flags=re.DOTALL)
    assert "return" not in prefix_no_comments, (
        "top-level `return` outside IIFE will throw 'Illegal return statement' in CDP"
    )


def test_js_no_leaky_const_at_top_level():
    """No bare `const X = ...` outside the IIFE body."""
    stripped = _js.strip()
    # Find IIFE bounds — opener is `(function` or `(()`
    m = re.match(r"^(?P<open>\(function|\(\(\s*=>\s*)", stripped)
    assert m is not None, "no IIFE opener"
    # Everything before the opening paren is the "outer" scope
    outer = stripped[: m.start()]
    # Disallow top-level const/let/var/function declarations in outer scope
    outer_no_comments = re.sub(r"//[^\n]*", "", outer)
    outer_no_comments = re.sub(r"/\*.*?\*/", "", outer_no_comments, flags=re.DOTALL)
    for kw in ("const ", "let ", "var ", "function "):
        assert kw not in outer_no_comments.strip(), (
            f"top-level `{kw.strip()}` leaks across CDP calls — re-declaration will throw"
        )
