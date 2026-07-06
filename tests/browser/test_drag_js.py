"""Browser-free logic proof for the drag-and-drop HTML5 JS.

``sandbox/scripts/sb_server.py`` can't be exercised here — SeleniumBase lives
only in the sandbox Docker image (not on the host), so a live ``/drag`` against
a real browser isn't runnable from the test suite. What we CAN prove without a
browser is the correctness of the JS we authored: ``SIMULATE_DND_JS`` is a pure
function of a ``document`` global, so we extract it from source, eval it in
Node against a hand-rolled stub DOM, and assert it dispatches the EXACT event
sequence that makes HTML5 drag-and-drop work:

* dragstart → dragenter → dragover → drop → dragend  (the chain Selenium's
  native ActionChains skips — the whole reason this exists).

If the JS ever drops a step, or dispatches to the wrong element, this fires.

The PHYSICS drag strategy is no longer a JS IIFE — it's the Python helper
``_trusted_cdp_drag`` driving the CDP Input domain directly, because synthetic
in-page MouseEvents can't move native sliders or fire dnd-kit's PointerSensor
(they're untrusted). That path is proven by the live E2E, not Node.
"""
from __future__ import annotations

import base64
import json
import re
import shutil
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
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

# No-legacy-left-behind: physics drag moved from a synthetic PHYS_DRAG_JS IIFE
# to the trusted CDP helper _trusted_cdp_drag. The OLD JS form must stay GONE —
# match the ASSIGNMENT, not the explanatory comment that names the migration.
assert not re.search(r"^PHYS_DRAG_JS\s*=\s*", _SOURCE, re.MULTILINE), (
    "PHYS_DRAG_JS assignment deleted — physics drag is now _trusted_cdp_drag (CDP Input)"
)

# Node harness: stub the DOM the IIFE touches, eval the constant (it's a bare
# arrow-fn expression), call it, and print the recorded events as JSON.
# JS body is base64'd in so no quoting/escaping can corrupt it.
_HARNESS = r"""
const DND = Buffer.from('__DND_B64__', 'base64').toString();

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

const dnd = (0, eval)(DND);
const dndResult = JSON.parse(dnd('.src', '.dst'));

console.log(JSON.stringify({
    dnd_ok: dndResult.ok,
    dnd_fired: dndResult.fired,
    dnd_src_events: els['.src']._events,
    dnd_dst_events: els['.dst']._events,
}));
"""


def _run_node() -> dict:
    script = _HARNESS.replace("__DND_B64__", base64.b64encode(_DND_JS.encode()).decode())
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
