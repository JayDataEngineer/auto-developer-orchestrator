"""Browser-free logic proof for the Phase 19 drag-and-drop JS.

``sandbox/scripts/sb_server.py`` can't be exercised here — SeleniumBase lives
only in the sandbox Docker image (not on the host), so a live ``/drag`` against
a real browser isn't runnable from the test suite. What we CAN prove without a
browser is the correctness of the JS we authored: ``SIMULATE_DND_JS`` and
``PHYS_DRAG_JS`` are pure functions of a ``document`` global, so we extract them
from source, eval them in Node against a hand-rolled stub DOM, and assert they
dispatch the EXACT event sequence that makes drag-and-drop work:

* HTML5 path: dragstart → dragenter → dragover → drop → dragend  (the chain
  Selenium's native ActionChains skips — the whole reason this exists).
* Physics path: mouseover → mousemove → mousedown → N×mousemove → mouseup.

If the JS ever drops a step, or dispatches to the wrong element, this fires.
The only thing it does NOT prove is that real browsers honor synthetic DragEvents
— that's a documented industry fact (Selenium issue #3604), not our code.
"""
from __future__ import annotations

import base64
import json
import re
import shutil
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[1]
SB_SERVER = REPO_ROOT / "sandbox" / "scripts" / "sb_server.py"

pytestmark = pytest.mark.skipif(
    shutil.which("node") is None,
    reason="node not on PATH — DnD JS logic test requires Node to eval the constants",
)


def _extract_const(source: str, name: str) -> str:
    # Pull the body of `NAME = r''' ... '''` (a triple-quoted raw string) as-is.
    m = re.search(rf'{name}\s*=\s*r?"""(.*?)"""', source, re.DOTALL)
    if not m:
        raise AssertionError(f"could not find {name} in sb_server.py")
    return m.group(1)


_SOURCE = SB_SERVER.read_text()
_DND_JS = _extract_const(_SOURCE, "SIMULATE_DND_JS")
_PHYS_JS = _extract_const(_SOURCE, "PHYS_DRAG_JS")

# Node harness: stub the DOM the two IIFEs touch, eval each constant (it's a
# bare arrow-fn expression), call it, and print the recorded events as JSON.
# JS bodies are base64'd in so no quoting/escaping can corrupt them.
_HARNESS = r"""
const DND = Buffer.from('__DND_B64__', 'base64').toString();
const PHYS = Buffer.from('__PHYS_B64__', 'base64').toString();

function makeEl(name) {
    return {
        tagName: { toLowerCase: () => 'div' },
        _name: name, _events: [],
        getBoundingClientRect: () => ({ left: 10, top: 20, width: 30, height: 40 }),
        dispatchEvent(e) { this._events.push(e.type); return true; },
    };
}
const els = { '.src': makeEl('src'), '.dst': makeEl('dst') };
const bodyEl = makeEl('body');

// The DnD JS passes `view: window` into event init dicts; some browsers key
// off it, so the JS names it unconditionally. Provide a non-null stub.
globalThis.window = globalThis;
globalThis.document = {
    querySelector: (sel) => els[sel] || null,
    elementFromPoint: () => bodyEl,
    createEvent: () => ({ initDragEvent() {} }),
};
globalThis.DataTransfer = class {
    constructor() { this.data = {}; }
    setData(k, v) { this.data[k] = v; }
    getData(k) { return this.data[k] || ''; }
};
globalThis.DragEvent = class {
    constructor(type, o = {}) { this.type = type; this.dataTransfer = o.dataTransfer || null; this.clientX = o.clientX || 0; this.clientY = o.clientY || 0; }
};
globalThis.MouseEvent = class {
    constructor(type, o = {}) { this.type = type; this.button = o.button || 0; this.buttons = o.buttons || 0; this.clientX = o.clientX || 0; this.clientY = o.clientY || 0; }
};

const dnd = (0, eval)(DND);
const phys = (0, eval)(PHYS);

const dndResult = JSON.parse(dnd('.src', '.dst'));
const physResult = JSON.parse(phys(10, 20, 110, 120, 5, 0));

console.log(JSON.stringify({
    dnd_ok: dndResult.ok,
    dnd_fired: dndResult.fired,
    dnd_src_events: els['.src']._events,
    dnd_dst_events: els['.dst']._events,
    phys_ok: physResult.ok,
    phys_types: physResult.fired.map((s) => s.split('(')[0]),
}));
"""


def _run_node() -> dict:
    script = (_HARNESS
              .replace("__DND_B64__", base64.b64encode(_DND_JS.encode()).decode())
              .replace("__PHYS_B64__", base64.b64encode(_PHYS_JS.encode()).decode()))
    proc = subprocess.run(
        ["node", "-"], input=script, text=True,
        capture_output=True, timeout=30,
    )
    if proc.returncode != 0:
        raise AssertionError(f"node failed (rc={proc.returncode}):\n{proc.stderr}")
    return json.loads(proc.stdout.strip())


def test_simulate_dnd_dispatches_full_html5_chain():
    """dragstart → dragenter → dragover → drop → dragend, in order."""
    out = _run_node()
    assert out["dnd_ok"] is True, out
    assert out["dnd_fired"] == ["dragstart", "dragenter", "dragover", "drop", "dragend"], out


def test_simulate_dnd_targets_right_elements():
    """Source receives dragstart + dragend; target receives the middle three.
    Dispatching the enter/over/drop on the SOURCE (a common bug) would
    silently no-op real drop handlers."""
    out = _run_node()
    assert out["dnd_src_events"] == ["dragstart", "dragend"], out
    assert out["dnd_dst_events"] == ["dragenter", "dragover", "drop"], out


def test_phys_drag_dispatches_mouse_sequence():
    """mouseover → mousemove → mousedown → (steps × mousemove) → mouseup.
    steps=5 ⇒ 1 lead mousemove + 5 interpolated = 6 mousemoves total."""
    out = _run_node()
    assert out["phys_ok"] is True, out
    expected = (["mouseover", "mousemove", "mousedown"]
                + ["mousemove"] * 5
                + ["mouseup"])
    assert out["phys_types"] == expected, out
