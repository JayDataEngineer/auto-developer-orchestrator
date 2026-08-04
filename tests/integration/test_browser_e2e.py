"""Live E2E for the SOTA browser toolkit + BrowserVisionMiddleware.

Drives the REAL harness path against REAL Chrome:
  DockerExecClient -> browser StructuredTool -> _sb_post -> curl ->
  sb_server.py -> live Chrome on DISPLAY :99.

Skipped unless ``PUX_E2E=1`` (needs the ``pux-sandbox`` image with Chrome +
sb_server up). Closes [[prepare-wiring-e2e-gap]] for the browser surface: the
unit tests prove JS-shape and decision logic; THIS proves real-Chrome behavior
the units structurally cannot — trusted-CDP drag moving a native ``<range>``
(synthetic ``dispatchEvent`` is provably unable to), a REAL DnD library
(SortableJS) reordering from our events, cross-frame iframe clicks, and the
``BrowserVisionMiddleware`` screenshot reaching the multimodal driver model
through the shipped gateway.

    PUX_E2E=1 uv run pytest tests/integration/test_browser_e2e.py -q
"""
from __future__ import annotations

import base64
import json
import os
import re
import time

import pytest

PUX_E2E = os.environ.get("PUX_E2E") == "1"
pytestmark = pytest.mark.skipif(
    not PUX_E2E,
    reason="set PUX_E2E=1 (pux-sandbox image with Chrome + sb_server) to run live browser E2E",
)

# Mirror ``bin/pux``'s ``set -a; . .env; set +a`` so a direct
# ``PUX_E2E=1 pytest`` run has the REAL provider key in env for the live
# multimodal-driver test below (we don't override anything already set —
# [[dont-fakekey-skip-e2e]]: never substitute a fake key to skip live model E2E).
if PUX_E2E:
    for _p in (".env", "./pux-harness/.env"):
        try:
            with open(_p) as _f:
                for _line in _f:
                    s = _line.strip()
                    if "=" in s and not s.startswith("#"):
                        _k, _v = s.split("=", 1)
                        os.environ.setdefault(
                            _k, _v.strip().strip('"').strip("'"))
        except FileNotFoundError:
            pass

# The test page, inlined so the test is fully self-contained (no /tmp fixture).
# Stages: 1 HTML5 DnD · 2 custom thumb · 3 NATIVE range · 4 SortableJS ·
# 5 hover · 6 keys · 7 below-the-fold · 8 iframe. ``window.rec`` is exposed so a
# same-origin iframe can call ``parent.rec(...)`` (a bare ``const`` at script
# scope is a lexical global, NOT a ``window`` property — the cross-frame trap).
_TEST_PAGE = """<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Pux SOTA Browser E2E</title>
<script src="https://cdn.jsdelivr.net/npm/sortablejs@1.15.2/Sortable.min.js"></script>
<style>
  body { font: 16px sans-serif; padding: 20px; }
  #log { white-space: pre-wrap; background:#111; color:#0f0; padding:10px; height:110px; overflow:auto; font-family:monospace;}
  .box { display:inline-block; width:120px; height:60px; border:2px solid #333; margin:8px; text-align:center; line-height:60px; vertical-align:middle; }
  #src { background:#cfe; } #dst { background:#fec; } #trigger { background:#eef; cursor:pointer; }
  #menu { display:none; background:#fee; padding:8px; margin:8px; }
  #track { position:relative; width:300px; height:30px; background:#eee; border:1px solid #999; margin:8px; }
  #thumb { position:absolute; left:0; top:0; width:30px; height:30px; background:#36c; cursor:grab; }
  #sortlist { list-style:none; padding:0; width:180px; }
  #sortlist li { background:#def; border:1px solid #88a; padding:8px; margin:4px; cursor:move; }
  .spacer { height:900px; }
</style>
</head>
<body>
<div id="log"></div>
<h3>1. HTML5 drag-and-drop (native draggable)</h3>
<div id="src" class="box" draggable="true">Drag me</div>
<div id="dst" class="box">Drop here</div>
<h3>2. Custom slider thumb (physics-drag target)</h3>
<div id="track"><div id="thumb"></div></div>
<span id="tval">left=0</span>
<h3>3. NATIVE range slider (trusted-CDP proof — synthetic events CAN'T move this)</h3>
<input id="range" type="range" min="0" max="100" value="20">
<span id="rval">range=20</span>
<h3>4. SortableJS list (real library — reorders on drag events)</h3>
<ul id="sortlist"><li id="iA">Item A</li><li id="iB">Item B</li><li id="iC">Item C</li></ul>
<span id="order">A,B,C</span>
<h3>5. Hover-revealed menu</h3>
<div id="trigger" class="box">Hover me</div>
<div id="menu">MENU REVEALED</div>
<h3>6. Key capture</h3>
<input id="keys" type="text"><span id="kreport"></span>
<h3>7. Below-the-fold target (scroll_into_view + click_at)</h3>
<div class="spacer"></div>
<button id="belowfold" onclick="rec('belowfold clicked')">deep button</button>
<h3>8. iframe (cross-frame element)</h3>
<iframe id="frm" srcdoc="<button id='inframe' onclick='parent.rec(&quot;iframe button clicked&quot;)'>in-frame button</button>" width="220" height="80"></iframe>
<script>
const log = document.getElementById('log');
const rec = s => { log.textContent += s + '\\n'; };
window.rec = rec;
const src = document.getElementById('src'), dst = document.getElementById('dst');
src.addEventListener('dragstart', e => { rec('dragstart fired'); try{e.dataTransfer.setData('text/plain','PAYLOAD');}catch(_){} });
dst.addEventListener('dragenter', e => { e.preventDefault(); rec('dragenter fired'); });
dst.addEventListener('dragover', e => { e.preventDefault(); rec('dragover fired'); });
dst.addEventListener('drop', e => { e.preventDefault(); let p='(none)'; try{p=e.dataTransfer.getData('text/plain')||'(empty)';}catch(_){} rec('drop fired; payload='+p); });
src.addEventListener('dragend', () => rec('dragend fired'));
const thumb = document.getElementById('thumb'), tval = document.getElementById('tval');
let dragging = false;
thumb.addEventListener('mousedown', () => { dragging = true; rec('thumb mousedown'); });
document.addEventListener('mousemove', e => { if(!dragging) return; const tr=document.getElementById('track').getBoundingClientRect(); let x=Math.max(0,Math.min(tr.width-30,e.clientX-tr.left)); thumb.style.left=x+'px'; tval.textContent='left='+Math.round(x); });
document.addEventListener('mouseup', () => { if(dragging){dragging=false; rec('thumb mouseup');} });
const range = document.getElementById('range'), rval = document.getElementById('rval');
range.addEventListener('input', () => { rval.textContent = 'range=' + range.value; rec('range input: ' + range.value); });
Sortable.create(document.getElementById('sortlist'), {
  forceFallback: true,
  onEnd: evt => { const ids = [...document.querySelectorAll('#sortlist li')].map(l => l.id.replace('i','')); document.getElementById('order').textContent = ids.join(','); rec('sortable onEnd: ' + ids.join(',')); },
});
const trigger = document.getElementById('trigger'), menu = document.getElementById('menu');
trigger.addEventListener('mouseenter', () => { menu.style.display='block'; rec('mouseenter -> menu shown'); });
document.getElementById('keys').addEventListener('keydown', e => { document.getElementById('kreport').textContent += e.key; rec('keydown:'+e.key); });
</script>
</body>
</html>
"""

_PNG_SIG = b"\x89PNG\r\n\x1a\n"


@pytest.fixture(scope="module")
def browser():
    """Boot the sandbox, build the browser specialists, stage the test page,
    and warm Chrome until the first navigate succeeds. Yields a dict of tools
    keyed by the slug after ``pux_sandbox_``."""
    from pux_harness.sandbox.docker_exec import DockerExecClient
    from pux_harness.sandbox.tools import build_native_specialists

    client = DockerExecClient()
    html_b64 = base64.b64encode(_TEST_PAGE.encode()).decode()
    staged, code = client.exec(
        f"echo {html_b64} | base64 -d > /sandbox/workspace/dnd_test.html && echo OK"
    )
    assert code == 0 and "OK" in staged, f"failed to stage test page: {staged!r}"

    specs = build_native_specialists(exec_client=client, vision_model=None, org=None)
    tools = {t.name.replace("pux_sandbox_", ""): t for t in specs}
    nav = tools["browser_navigate"]

    # warm sb_server + Chrome: navigate retries until the page paints
    last_err = None
    for _ in range(30):
        try:
            nav.invoke({"url": "file:///sandbox/workspace/dnd_test.html"})
            time.sleep(0.5)
            last_err = None
            break
        except Exception as e:  # sb_server still booting
            last_err = e
            time.sleep(1)
    assert last_err is None, f"Chrome never came up: {last_err}"
    yield tools


def _js(browser, expr):
    """/evaluate returns {"ok":true,"result":"<str>","type":"str"}; pull .result."""
    raw = browser["browser_evaluate"].invoke({"code": "return " + expr}) or ""
    try:
        return json.loads(raw).get("result", "")
    except Exception:
        return raw


def _log(browser):
    return _js(browser, "document.getElementById('log').textContent") or ""


def test_browser_sota_capabilities_live(browser):
    """All 8 SOTA capabilities against real Chrome, in one session. Each stage
    asserts a behavior a unit test cannot prove (trusted-CDP default action,
    real-library reordering, cross-frame bridge)."""
    B = browser
    nav = B["browser_navigate"]

    # navigate twice: load, then reset the log to a clean state for the run
    nav.invoke({"url": "file:///sandbox/workspace/dnd_test.html"})
    nav.invoke({"url": "file:///sandbox/workspace/dnd_test.html"})

    # [1] HTML5 DnD — auto strategy -> html5 chain fires dragstart..drop
    r = B["browser_drag"].invoke({"from_selector": "#src", "to_selector": "#dst", "strategy": "auto"})
    method = ""
    try:
        method = json.loads(r or "{}").get("drag_method", "?")
    except Exception:
        pass
    lt = _log(B)
    assert "drop fired" in lt and "dragstart fired" in lt, (
        f"HTML5 DnD chain did not complete (method={method}); log=\n{lt}"
    )

    # [2] HEADLINE: native <range> moves under trusted-CDP physics drag.
    # Synthetic dispatchEvent CANNOT move a native range — Chrome computes the
    # value from the default action of an isTrusted=true pointer sequence.
    before = _js(B, "document.getElementById('range').value")
    B["browser_drag"].invoke({"from_selector": "#range", "dx": 220, "dy": 0, "strategy": "physics", "steps": 30})
    after = _js(B, "document.getElementById('range').value")
    assert int(after) > int(before) + 5, (
        f"native range did not move under trusted-CDP drag ({before}->{after}) — "
        "isTrusted events not landing"
    )

    # [3] custom-thumb physics drag
    tb = _js(B, "document.getElementById('tval').textContent")
    B["browser_drag"].invoke({"from_selector": "#thumb", "dx": 160, "dy": 0, "strategy": "physics", "steps": 20})
    ta = _js(B, "document.getElementById('tval').textContent")
    assert int(re.search(r"\d+", ta).group()) > int(re.search(r"\d+", tb).group()), (
        f"custom thumb did not move ({tb}->{ta})"
    )

    # [4] SortableJS — a REAL production DnD library reorders from our events
    before = _js(B, "document.getElementById('order').textContent")
    B["browser_drag"].invoke({"from_selector": "#iC", "to_selector": "#iA", "strategy": "physics", "steps": 25})
    after = _js(B, "document.getElementById('order').textContent")
    assert after != before and "C" in (after or ""), (
        f"SortableJS list did not reorder ({before}->{after})"
    )

    # [5] hover reveals a menu (mouseenter)
    B["browser_hover"].invoke({"selector": "#trigger"})
    disp = _js(B, "getComputedStyle(document.getElementById('menu')).display")
    assert "block" in str(disp), f"hover did not reveal menu (display={disp!r})"

    # [6] press keys into an input
    B["browser_press"].invoke({"keys": "h", "selector": "#keys"})
    B["browser_press"].invoke({"keys": "i", "selector": "#keys"})
    rep = _js(B, "document.getElementById('kreport').textContent")
    assert "h" in str(rep) and "i" in str(rep), f"keys not captured ({rep!r})"

    # [7] scroll_into_view + click_at on a below-the-fold button
    def _inview():
        return _js(B, "(()=>{const r=document.getElementById('belowfold').getBoundingClientRect();return (r.top>=0&&r.bottom<=window.innerHeight)?'inview':'offscreen';})()")
    before_iv = _inview()
    B["browser_scroll_into_view"].invoke({"selector": "#belowfold"})
    after_iv = _inview()
    B["browser_click_at"].invoke({"selector": "#belowfold"})
    assert before_iv == "offscreen" and after_iv == "inview" and "belowfold clicked" in _log(B), (
        f"scroll_into_view/click_at failed ({before_iv}->{after_iv})"
    )

    # [8] iframe — click an in-frame element via the contentDocument bridge
    B["browser_iframe"].invoke({"selector": "#frm", "action": "click", "inner_selector": "#inframe"})
    assert "iframe button clicked" in _log(B), "iframe in-frame click did not register"


def test_browser_vision_middleware_attaches_real_screenshot(browser):
    """BrowserVisionMiddleware fetches the REAL screenshot_path out of the
    container and emits a Command([text ToolMessage, HumanMessage(image)]).
    The image block decodes to a viewport-sized PNG (not a stub)."""
    from types import SimpleNamespace

    from langchain_core.messages import HumanMessage, ToolMessage
    from langgraph.types import Command

    from pux_harness.context.browser_vision import BrowserVisionMiddleware
    from pux_harness.sandbox.docker_exec import DockerExecClient

    nav = browser["browser_navigate"]
    shot = browser["browser_screenshot"]
    nav.invoke({"url": "https://example.com"})
    time.sleep(1.5)  # let it paint

    raw = shot.invoke({}) or ""
    payload = json.loads(raw)
    assert payload.get("screenshot_path"), f"no screenshot_path in result: {raw[:200]}"

    tm_in = ToolMessage(content=raw, tool_call_id="c1",
                        name="pux_sandbox_browser_screenshot", status="success")
    mw = BrowserVisionMiddleware(DockerExecClient())
    out = mw.wrap_tool_call(
        SimpleNamespace(tool_call={"name": "pux_sandbox_browser_screenshot", "id": "c1", "args": {}}),
        lambda _r: tm_in,
    )

    assert isinstance(out, Command), f"middleware returned {type(out).__name__}, not Command"
    msgs = out.update.get("messages", [])
    assert len(msgs) == 2, f"expected [ToolMessage, HumanMessage], got {len(msgs)}"
    text_tm, human = msgs
    assert isinstance(text_tm, ToolMessage) and text_tm.content == raw  # text result preserved
    assert isinstance(human, HumanMessage)
    img = next(b for b in human.content if b["type"] == "image")
    assert img["mime_type"] == "image/png"
    png = base64.b64decode(img["base64"])
    assert png[:8] == _PNG_SIG, "image block does not decode to a PNG"
    assert len(png) > 10_000, f"screenshot suspiciously small ({len(png)} B)"


def test_browser_vision_reaches_multimodal_driver(browser):
    """The companion HumanMessage(image) reaches the shipped multimodal driver
    (mimo-v2.5) through the gateway and the model SEES the rendered page —
    referencing content visible only in the PNG, not the tool's text JSON.
    The text-only ToolMessage shape is what makes the gateway accept it (it 400s
    on image-in-tool). Skipped without ``OPENROUTER_API_KEY`` (live model call)."""
    from types import SimpleNamespace

    from langchain_core.messages import AIMessage, HumanMessage, ToolMessage

    from pux_harness.agent.model import get_model
    from pux_harness.context.browser_vision import BrowserVisionMiddleware
    from pux_harness.sandbox.docker_exec import DockerExecClient

    if not os.environ.get("OPENROUTER_API_KEY"):
        pytest.skip("OPENROUTER_API_KEY not set — cannot run live multimodal model call")

    nav = browser["browser_navigate"]
    shot = browser["browser_screenshot"]
    nav.invoke({"url": "https://example.com"})
    time.sleep(1.5)

    raw = shot.invoke({}) or ""
    tm_in = ToolMessage(content=raw, tool_call_id="c1",
                        name="pux_sandbox_browser_screenshot", status="success")
    mw = BrowserVisionMiddleware(DockerExecClient())
    out = mw.wrap_tool_call(
        SimpleNamespace(tool_call={"name": "pux_sandbox_browser_screenshot", "id": "c1", "args": {}}),
        lambda _r: tm_in,
    )
    text_tm, human_img = out.update["messages"]

    model = get_model(role="base", org="general")
    if hasattr(model, "bind"):
        model = model.bind(max_tokens=300)
    # the exact conversation the live agent loop assembles from the Command
    convo = [
        HumanMessage(content="I just took a browser screenshot. In ONE short sentence, what does the page show?"),
        AIMessage(content="", tool_calls=[{"name": "pux_sandbox_browser_screenshot",
                                           "args": {}, "id": "c1", "type": "tool"}]),
        text_tm, human_img,
    ]
    resp = model.invoke(convo)
    text = (resp.content if isinstance(resp.content, str)
            else "".join(b.get("text", "") for b in resp.content if isinstance(b, dict)))
    low = (text or "").lower()
    assert any(w in low for w in ("example", "domain", "illustrative")), (
        f"model did not reference screenshot-only content — image not seen? response={text!r}"
    )


def test_mimo_drives_browser_with_vision_full_loop(browser):
    """The headline capability, proven end-to-end in the REAL deepagents loop:
    MiMo-2.5 is the driver, it EMITS ``pux_sandbox_browser_*`` tool calls itself,
    BrowserVision fires inside the real ToolNode path (not a hand-built request),
    and the model reads the attached screenshot to answer.

    VISION-ONLY probe: a staged page has a distinctly-colored box whose COLOR is
    NOT in the navigate tool's text JSON (only url/title/text). If the model
    names the color, it SAW the screenshot — no other path to that info. This is
    strictly stronger than ``test_browser_vision_reaches_multimodal_driver``
    (which uses a hand-built conversation); this one proves the model decides to
    look AND sees the result. Skipped without ``OPENROUTER_API_KEY``."""
    from deepagents import create_deep_agent
    from langchain_core.messages import HumanMessage

    from pux_harness.agent.model import get_model
    from pux_harness.context.browser_vision import BrowserVisionMiddleware
    from pux_harness.sandbox.docker_exec import DockerExecClient

    if not os.environ.get("OPENROUTER_API_KEY"):
        pytest.skip("OPENROUTER_API_KEY not set — cannot run live multimodal model call")

    # stage a vision-only page: text "VISION TARGET" lands in the tool JSON,
    # but the COLOR (#e91e63 = pink/magenta) does NOT — only the PNG has it.
    client = DockerExecClient()
    page = (b"<!DOCTYPE html><html><head><meta charset='utf-8'><title>v</title>"
            b"<style>body{margin:0;padding:40px;background:#222;font-family:sans-serif}"
            b"#box{background:#e91e63;color:#fff;padding:30px;font-size:28px;"
            b"font-weight:bold;border-radius:8px;width:300px;text-align:center}"
            b"</style></head><body><div id='box'>VISION TARGET</div></body></html>")
    page_b64 = base64.b64encode(page).decode()
    staged, sc = client.exec(
        f"echo {page_b64} | base64 -d > /sandbox/workspace/vision.html && echo OK")
    assert sc == 0 and "OK" in staged

    specs = [t for t in browser.values() if t.name.startswith("pux_sandbox_browser_")]
    model = get_model(role="base", org="general")
    agent = create_deep_agent(
        model=model,
        tools=specs,
        middleware=[BrowserVisionMiddleware(client)],
        system_prompt=(
            "You drive a browser via the pux_sandbox_browser_* tools. "
            "Use them to look at pages and answer questions about what you see."
        ),
        checkpointer=False,
    )
    task = (
        "Use the browser tools to navigate to file:///sandbox/workspace/vision.html, "
        "take a screenshot, and tell me: what COLOR is the big box on the page? "
        "Answer in one short sentence naming the color, then stop."
    )
    result = agent.invoke(
        {"messages": [HumanMessage(content=task)]},
        config={"recursion_limit": 60},
    )
    msgs = result["messages"]

    # 1. the model emitted a browser tool call itself (it drove)
    calls = [tc["name"] for m in msgs for tc in (getattr(m, "tool_calls", None) or [])
             if str(tc.get("name", "")).startswith("pux_sandbox_browser_")]
    assert calls, "MiMo-2.5 emitted no browser tool call — it did not drive"
    # 2. BrowserVision fired in the real loop (an image block reached the model)
    images = sum(
        1 for m in msgs if isinstance(m.content, list)
        and any(isinstance(b, dict) and b.get("type") == "image" for b in m.content)
    )
    assert images >= 1, "BrowserVision injected no screenshot in the real loop"
    # 3. the model named the vision-only color (only knowable from the screenshot)
    final = msgs[-1]
    text = (final.content if isinstance(final.content, str)
            else "".join(b.get("text", "") for b in final.content if isinstance(b, dict)))
    low = (text or "").lower()
    assert any(tok in low for tok in ("pink", "magenta", "fuchsia", "purpl", "rose")), (
        f"model did not name the vision-only box color — did not see screenshot? "
        f"calls={calls} answer={text!r}"
    )


# --- a REAL third-party challenge: can MiMo-2.5 solve it autonomously? -------
SB_DD_URL = "https://seleniumbase.io/w3schools/drag_drop"


def _iframe_drag1_parent(browser) -> str:
    """Read where ``#drag1`` currently lives INSIDE the ``iframeResult`` doc.
    Same-origin → reachable from the top doc via contentDocument. Returns the
    parent id ('div1' = solved), or '' if the image / iframe is absent."""
    from types import SimpleNamespace  # noqa: F401  (kept for parity w/ above)
    js = ("(()=>{const f=document.getElementById('iframeResult');"
          "const img=f&&f.contentDocument&&f.contentDocument.getElementById('drag1');"
          "return img?(img.parentElement&&img.parentElement.id||'(no parent)'):'(no drag1)';})()")
    raw = browser["browser_evaluate"].invoke({"code": "return " + js}) or ""
    try:
        r = json.loads(raw)
        return r.get("result", "") if isinstance(r, dict) else str(r)
    except Exception:
        return raw


def test_mimo_solves_real_dragdrop_challenge(browser):
    """The capability eval: can MiMo-2.5, given the browser tools + vision,
    SOLVE a real third-party drag-and-drop challenge on its own — no hand-held
    selectors, no scripted drag.

    The page (https://seleniumbase.io/w3schools/drag_drop) is the classic
    W3Schools demo: drag the ``#drag1`` image into the ``#div1`` rectangle,
    BOTH rendered inside an ``iframeResult`` iframe. The drag tools cannot
    reach iframe content by selector and coordinate-physics does not trigger
    HTML5 DnD's drop, so a viable solve requires the model to (a) recognize
    the iframe, and (b) drive the in-iframe DOM — e.g. via ``browser_iframe``
    evaluate dispatching the HTML5 DragEvent chain (proven viable by hand).
    The model has to DISCOVER that; the task prompt gives only the goal.

    Verdict is read from the DOM, model-independent: solved iff
    ``#drag1``'s parent is ``#div1``. Skipped without ``OPENROUTER_API_KEY``."""
    from deepagents import create_deep_agent
    from langchain_core.messages import HumanMessage

    from pux_harness.agent.model import get_model
    from pux_harness.context.browser_vision import BrowserVisionMiddleware
    from pux_harness.sandbox.docker_exec import DockerExecClient

    if not os.environ.get("OPENROUTER_API_KEY"):
        pytest.skip("OPENROUTER_API_KEY not set — cannot run live multimodal model call")

    nav = browser["browser_navigate"]
    for _ in range(30):
        try:
            nav.invoke({"url": SB_DD_URL}); break
        except Exception:
            time.sleep(1)
    time.sleep(2.5)
    assert "div1" not in (_iframe_drag1_parent(browser) or ""), (
        "challenge pre-state unexpected — drag1 already inside div1 before solving?")

    specs = [t for t in browser.values() if t.name.startswith("pux_sandbox_browser_")]
    model = get_model(role="base", org="general")
    # BrowserVision needs a real exec client to base64 screenshots out of the
    # container; it shares the same container as the fixture's client.
    vision_client = DockerExecClient()
    agent = create_deep_agent(
        model=model,
        tools=specs,
        middleware=[BrowserVisionMiddleware(vision_client)],
        system_prompt=(
            "You drive a browser via the pux_sandbox_browser_* tools to accomplish "
            "tasks. You can navigate, take screenshots to SEE the page, run JavaScript "
            "in the page (browser_evaluate) or inside iframes (browser_iframe with "
            "action='evaluate'), list iframes, and drag elements (browser_drag). "
            "Inspect the page, form a plan, act, then verify the result in the DOM "
            "before declaring success. Be resourceful — if one approach fails, try "
            "another."
        ),
        checkpointer=False,
    )
    task = (
        f"Go to {SB_DD_URL} in the browser. The page presents a drag-and-drop "
        "challenge: the W3Schools image must be dragged into the rectangle. "
        "Solve the challenge so that the image ends up inside the rectangle. "
        "Inspect the page structure first, then act, then VERIFY in the DOM "
        "that the image is now inside the rectangle. Report whether you succeeded."
    )
    result = agent.invoke(
        {"messages": [HumanMessage(content=task)]},
        config={"recursion_limit": 120},
    )
    msgs = result["messages"]

    # diagnostics: what did the model try?
    calls = [tc["name"] for m in msgs for tc in (getattr(m, "tool_calls", None) or [])
             if str(tc.get("name", "")).startswith("pux_sandbox_browser_")]
    final = msgs[-1]
    answer = (final.content if isinstance(final.content, str)
              else "".join(b.get("text", "") for b in final.content if isinstance(b, dict)))
    print(f"\n[dragdrop eval] tool calls: {calls}")
    print(f"[dragdrop eval] final answer: {(answer or '').strip()[:200]!r}")

    # verdict: model-independent DOM check
    parent = _iframe_drag1_parent(browser)
    print(f"[dragdrop eval] #drag1 parent after run: {parent!r}")
    assert parent == "div1", (
        f"MiMo-2.5 did NOT solve the challenge (drag1 not inside div1; "
        f"parent={parent!r}). calls={calls} answer={answer!r}"
    )
