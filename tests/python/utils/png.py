"""PNG validation helpers — deduplicated from multiple test files."""

import base64


def is_valid_png(data: bytes) -> bool:
    """Check if bytes start with PNG magic header."""
    return data[:8] == b'\x89PNG\r\n\x1a\n'


def decode_b64_png(b64_string: str) -> bytes:
    """Decode a base64 PNG string, raising ValueError if not valid PNG."""
    data = base64.b64decode(b64_string)
    if not is_valid_png(data):
        raise ValueError("Decoded data is not a valid PNG")
    return data
