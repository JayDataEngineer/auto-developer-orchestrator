"""Unit tests for ReadFileVisionMiddleware — the automatic image/binary
fallback for non-multimodal drivers.

When ``read_file`` (or any tool) returns a ToolMessage with image/binary content
blocks, a non-multimodal model cannot consume them → gateway HTTP 400. This
middleware intercepts the ToolMessage and replaces the binary block with a text
pointer to ``describe_image`` (the multimodal-role specialist tool), so a
text-only model reaches vision through a typed tool call instead of crashing.
"""
from langchain_core.messages import ToolMessage

from deepagents_context.read_file_vision import (
    ReadFileVisionMiddleware,
    _has_binary_blocks,
)


# --- _has_binary_blocks helper ---

def test_has_binary_blocks_detects_image():
    assert _has_binary_blocks([{"type": "image", "base64": "ABC"}])

def test_has_binary_blocks_detects_file():
    assert _has_binary_blocks([{"type": "file", "base64": "ABC"}])

def test_has_binary_blocks_detects_video_frame():
    assert _has_binary_blocks([{"type": "video_frame", "base64": "ABC"}])

def test_has_binary_blocks_passes_text():
    assert not _has_binary_blocks([{"type": "text", "text": "hello"}])

def test_has_binary_blocks_passes_string():
    assert not _has_binary_blocks("plain string")

def test_has_binary_blocks_passes_none():
    assert not _has_binary_blocks(None)

def test_has_binary_blocks_mixed_blocks():
    # A list with both text and image → detected
    assert _has_binary_blocks([
        {"type": "text", "text": "description"},
        {"type": "image", "base64": "XYZ", "mime_type": "image/png"},
    ])


# --- Non-multimodal driver: image replaced with text pointer ---

def test_non_multimodal_image_replaced_with_pointer():
    """The core case: read_file on a PNG with a non-multimodal driver →
    the image block is replaced with a describe_image text pointer."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "iVBORw0KGgo=", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_abc",
        additional_kwargs={"read_file_path": "/sandbox/screenshot.png",
                           "read_file_media_type": "image/png"},
        status="success",
    )
    result = mw._intercept(tm)
    assert isinstance(result, ToolMessage)
    assert isinstance(result.content, str)
    assert "/sandbox/screenshot.png" in result.content
    assert "describe_image" in result.content
    assert result.tool_call_id == "call_abc"
    assert result.name == "read_file"


def test_non_multimodal_pdf_replaced_with_pointer():
    """PDF files are also binary — a non-multimodal model can't read them
    either. The block type is 'file', not 'image', but the interception fires
    for all non-text block types."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    tm = ToolMessage(
        content=[{"type": "file", "base64": "JVBERi0=", "mime_type": "application/pdf"}],
        name="read_file",
        tool_call_id="call_pdf",
        additional_kwargs={"read_file_path": "/sandbox/doc.pdf",
                           "read_file_media_type": "application/pdf"},
        status="success",
    )
    result = mw._intercept(tm)
    assert isinstance(result.content, str)
    assert "/sandbox/doc.pdf" in result.content
    assert "describe_image" in result.content


def test_non_multimodal_image_without_path_uses_tool_call_id():
    """When additional_kwargs has no read_file_path (a non-read_file tool),
    the pointer still fires — it uses the tool_call_id as a label."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC", "mime_type": "image/jpeg"}],
        name="some_other_tool",
        tool_call_id="call_xyz",
        status="success",
    )
    result = mw._intercept(tm)
    assert isinstance(result.content, str)
    assert "describe_image" in result.content
    assert "call_xyz" in result.content


def test_non_multimodal_text_file_passes_through():
    """Text ToolMessages (content is a string) are never intercepted."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    tm = ToolMessage(
        content="line 1: hello\nline 2: world",
        name="read_file",
        tool_call_id="call_text",
        status="success",
    )
    result = mw._intercept(tm)
    assert result is tm


def test_non_multimodal_non_toolmessage_passes_through():
    """Non-ToolMessage results (strings, dicts, etc.) pass through."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    assert mw._intercept("some string") == "some string"
    assert mw._intercept({"key": "val"}) == {"key": "val"}


# --- Multimodal driver: everything passes through ---

def test_multimodal_image_passes_through():
    """A multimodal driver can read image blocks natively — no interception."""
    mw = ReadFileVisionMiddleware(multimodal_driver=True)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "iVBORw0KGgo=", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_abc",
        additional_kwargs={"read_file_path": "/sandbox/img.png"},
        status="success",
    )
    result = mw._intercept(tm)
    assert result is tm  # identity — completely unchanged


def test_multimodal_text_passes_through():
    mw = ReadFileVisionMiddleware(multimodal_driver=True)
    tm = ToolMessage(content="hello", name="read_file", tool_call_id="c", status="success")
    assert mw._intercept(tm) is tm


# --- wrap_tool_call / awrap_tool_call delegation ---

def test_wrap_tool_call_intercepts_for_non_multimodal():
    """wrap_tool_call is the real hook. The handler's result is intercepted."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)

    image_tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_wrap",
        additional_kwargs={"read_file_path": "/sandbox/x.png"},
        status="success",
    )
    request = type("Req", (), {"tool_call": {"name": "read_file"}})()
    result = mw.wrap_tool_call(request, lambda req: image_tm)
    assert isinstance(result, ToolMessage)
    assert isinstance(result.content, str)
    assert "/sandbox/x.png" in result.content


def test_wrap_tool_call_passes_through_for_multimodal():
    """Multimodal driver → wrap_tool_call is a clean no-op pass-through."""
    mw = ReadFileVisionMiddleware(multimodal_driver=True)
    image_tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC"}],
        name="read_file",
        tool_call_id="c",
        status="success",
    )
    request = type("Req", (), {"tool_call": {"name": "read_file"}})()
    result = mw.wrap_tool_call(request, lambda req: image_tm)
    assert result is image_tm


def test_disabled_middleware_passes_everything():
    """enabled=False → the middleware never intercepts, even for non-MM."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False, enabled=False)
    image_tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC"}],
        name="read_file",
        tool_call_id="c",
        status="success",
    )
    request = type("Req", (), {"tool_call": {"name": "read_file"}})()
    result = mw.wrap_tool_call(request, lambda req: image_tm)
    assert result is image_tm


# --- async ---

import asyncio


def test_awrap_tool_call_intercepts_for_non_multimodal():
    """awrap_tool_call is the async hook. Same interception logic."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    image_tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_async",
        additional_kwargs={"read_file_path": "/sandbox/a.png"},
        status="success",
    )
    request = type("Req", (), {"tool_call": {"name": "read_file"}})()

    async def _handler(req):
        return image_tm

    result = asyncio.run(mw.awrap_tool_call(request, _handler))
    assert isinstance(result, ToolMessage)
    assert isinstance(result.content, str)
    assert "/sandbox/a.png" in result.content


# --- auto-describe mode (vision_model provided) ---

class _FakeVisionModel:
    """Fake vision model that returns a canned description for any image."""
    def __init__(self, description="A red rectangle with text.", fail=False):
        self._description = description
        self._fail = fail
        self.invoked = False

    def invoke(self, messages):
        self.invoked = True
        if self._fail:
            raise RuntimeError("vision API error")
        return type("Resp", (), {"content": self._description})()

    async def ainvoke(self, messages):
        return self.invoke(messages)


def test_auto_describe_replaces_image_with_description():
    """When vision_model is set + driver is non-multimodal, the middleware
    calls the vision model and returns its DESCRIPTION, not a text pointer."""
    vision = _FakeVisionModel(description="A white image with 'HELLO' in red text.")
    mw = ReadFileVisionMiddleware(multimodal_driver=False, vision_model=vision)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "iVBORw0KGgo=", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_auto",
        additional_kwargs={"read_file_path": "/sandbox/img.png"},
        status="success",
    )
    result = mw._intercept(tm)
    assert vision.invoked, "Vision model should have been called"
    assert isinstance(result, ToolMessage)
    assert isinstance(result.content, str)
    assert "HELLO" in result.content  # the description text
    assert "/sandbox/img.png" in result.content  # the path prefix
    # The raw base64 should NOT be in the content
    assert "iVBORw0KGgo=" not in result.content


def test_auto_describe_fallback_to_pointer_on_failure():
    """When the vision model call FAILS, the middleware falls back to the text
    pointer so the model still gets a steer instead of crashing."""
    vision = _FakeVisionModel(fail=True)
    mw = ReadFileVisionMiddleware(multimodal_driver=False, vision_model=vision)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "iVBORw0KGgo=", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_fail",
        additional_kwargs={"read_file_path": "/sandbox/fail.png"},
        status="success",
    )
    result = mw._intercept(tm)
    assert vision.invoked, "Vision model should have been attempted"
    assert isinstance(result, ToolMessage)
    assert isinstance(result.content, str)
    assert "describe_image" in result.content  # fell back to text pointer


def test_auto_describe_no_vision_model_uses_text_pointer():
    """When vision_model is None (no model available), the middleware uses the
    text pointer directly without attempting a vision call."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False, vision_model=None)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_novm",
        additional_kwargs={"read_file_path": "/sandbox/novm.png"},
        status="success",
    )
    result = mw._intercept(tm)
    assert isinstance(result.content, str)
    assert "describe_image" in result.content  # text pointer, not description


def test_auto_describe_multimodal_driver_skips():
    """Even with a vision_model set, a multimodal driver never triggers
    auto-describe — the image passes through natively."""
    vision = _FakeVisionModel()
    mw = ReadFileVisionMiddleware(multimodal_driver=True, vision_model=vision)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_mm",
        status="success",
    )
    result = mw._intercept(tm)
    assert result is tm  # unchanged
    assert not vision.invoked, "Vision model should NOT be called for multimodal driver"


def test_auto_describe_extracts_correct_mime_type():
    """The middleware passes the correct mime_type to the vision model via the
    canonical image content block format."""
    captured_blocks = []

    class _BlockChecker:
        def invoke(self, messages):
            for msg in messages:
                if hasattr(msg, 'content') and isinstance(msg.content, list):
                    for block in msg.content:
                        if isinstance(block, dict) and block.get("type") == "image":
                            captured_blocks.append(block)
            return type("Resp", (), {"content": "An image."})()

        async def ainvoke(self, messages):
            return self.invoke(messages)

    vision = _BlockChecker()
    mw = ReadFileVisionMiddleware(multimodal_driver=False, vision_model=vision)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "ABCDE", "mime_type": "image/jpeg"}],
        name="read_file",
        tool_call_id="call_mime",
        additional_kwargs={"read_file_path": "/sandbox/photo.jpg"},
        status="success",
    )
    mw._intercept(tm)
    assert len(captured_blocks) == 1
    assert captured_blocks[0]["mime_type"] == "image/jpeg"
    assert captured_blocks[0]["base64"] == "ABCDE"


def test_auto_describe_async():
    """The async path also auto-describes correctly."""
    vision = _FakeVisionModel(description="Async description works.")
    mw = ReadFileVisionMiddleware(multimodal_driver=False, vision_model=vision)
    tm = ToolMessage(
        content=[{"type": "image", "base64": "ABC", "mime_type": "image/png"}],
        name="read_file",
        tool_call_id="call_async_desc",
        additional_kwargs={"read_file_path": "/sandbox/async.png"},
        status="success",
    )
    result = asyncio.run(mw._intercept_async(tm))
    assert vision.invoked
    assert isinstance(result, ToolMessage)
    assert "Async description works." in result.content


# --- Layer 2: wrap_model_call safety net ---

from langchain_core.messages import HumanMessage as _HM
from deepagents_context.read_file_vision import _strip_binary_blocks

# Alias so test functions can use HumanMessage without polluting the top-level
# import section (ToolMessage is already imported at the top).
HumanMessage = _HM


class _FakeRequest:
    """Minimal stand-in for the langgraph ModelRequest, supporting override()."""
    def __init__(self, messages):
        self.messages = messages

    def override(self, *, messages):
        return _FakeRequest(messages)


def test_strip_binary_blocks_replaces_image():
    """_strip_binary_blocks replaces image blocks with text placeholders."""
    content = [
        {"type": "text", "text": "hello"},
        {"type": "image", "base64": "ABCDEF", "mime_type": "image/png"},
    ]
    new_content, changed = _strip_binary_blocks(content)
    assert changed
    assert all(b["type"] == "text" for b in new_content)
    assert len(new_content) == 2
    assert "binary/image content removed" in new_content[1]["text"]


def test_strip_binary_blocks_preserves_text():
    """All-text content is unchanged."""
    content = [{"type": "text", "text": "hello"}]
    new_content, changed = _strip_binary_blocks(content)
    assert not changed
    assert new_content is content  # identity — no copy needed


def test_strip_binary_blocks_preserves_string():
    """String content (not a list) passes through."""
    new_content, changed = _strip_binary_blocks("plain string")
    assert not changed
    assert new_content == "plain string"


def test_safety_net_strips_image_from_human_message():
    """Layer 2: wrap_model_call strips image blocks from HumanMessages before
    the model sees them — catches leaks from paths that bypassed Layer 1."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    messages = [
        HumanMessage(content=[
            {"type": "text", "text": "what is this?"},
            {"type": "image", "base64": "iVBORw0KGgo=", "mime_type": "image/png"},
        ]),
    ]
    request = _FakeRequest(messages)
    # The handler captures what the model actually sees
    captured = []
    def handler(req):
        captured.extend(req.messages)
        return "model_response"
    mw.wrap_model_call(request, handler)
    # The model should NOT see any image blocks
    for msg in captured:
        if isinstance(msg.content, list):
            for block in msg.content:
                assert block.get("type") != "image", "Image block leaked to model!"


def test_safety_net_strips_image_from_tool_message():
    """Layer 2 catches ToolMessages with image blocks that somehow bypassed
    Layer 1 (e.g. loaded from a pre-existing thread in the checkpointer)."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    messages = [
        ToolMessage(
            content=[{"type": "image", "base64": "ABC", "mime_type": "image/png"}],
            name="read_file",
            tool_call_id="leaked",
            status="success",
        ),
    ]
    request = _FakeRequest(messages)
    captured = []
    def handler(req):
        captured.extend(req.messages)
        return "ok"
    mw.wrap_model_call(request, handler)
    # No image blocks should reach the model
    for msg in captured:
        if isinstance(msg.content, list):
            assert all(b.get("type") == "text" for b in msg.content), \
                "Binary block leaked through safety net!"


def test_safety_net_noop_for_text_messages():
    """When all messages are text-only, the safety net is a no-op (identity)."""
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    messages = [
        HumanMessage(content="hello"),
        ToolMessage(content="tool result", name="t", tool_call_id="c", status="success"),
    ]
    request = _FakeRequest(messages)
    called = [False]
    def handler(req):
        called[0] = True
        return "ok"
    mw.wrap_model_call(request, handler)
    assert called[0]


def test_safety_net_noop_for_multimodal_driver():
    """For multimodal drivers, the safety net is a no-op — images pass through."""
    mw = ReadFileVisionMiddleware(multimodal_driver=True)
    messages = [
        HumanMessage(content=[
            {"type": "text", "text": "describe this"},
            {"type": "image", "base64": "ABC", "mime_type": "image/png"},
        ]),
    ]
    request = _FakeRequest(messages)
    captured = []
    def handler(req):
        captured.extend(req.messages)
        return "ok"
    mw.wrap_model_call(request, handler)
    # The image should STILL be there for multimodal drivers
    assert any(
        isinstance(b, dict) and b.get("type") == "image"
        for msg in captured
        if isinstance(getattr(msg, 'content', None), list)
        for b in msg.content
    )


def test_safety_net_strips_mixed_message_types():
    """The safety net handles a mix of HumanMessage, ToolMessage, and AIMessage
    — stripping binary blocks from ALL of them."""
    from langchain_core.messages import AIMessage
    mw = ReadFileVisionMiddleware(multimodal_driver=False)
    messages = [
        HumanMessage(content=[{"type": "image", "base64": "H1"}]),
        ToolMessage(content=[{"type": "image", "base64": "T1"}],
                    name="read_file", tool_call_id="c1", status="success"),
        AIMessage(content=[{"type": "text", "text": "ok"}, {"type": "image", "base64": "A1"}]),
        HumanMessage(content="plain text"),  # should pass through unchanged
    ]
    request = _FakeRequest(messages)
    captured = []
    def handler(req):
        captured.extend(req.messages)
        return "ok"
    mw.wrap_model_call(request, handler)
    for msg in captured:
        content = getattr(msg, 'content', None)
        if isinstance(content, list):
            for block in content:
                assert block.get("type") not in ("image", "file", "video_frame"), \
                    f"Binary block leaked in {type(msg).__name__}!"
