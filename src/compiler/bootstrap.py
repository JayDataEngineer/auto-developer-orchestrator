"""Process bootstrap shared by every pux entrypoint and exported runners.

Loads ``./.env`` via ``find_dotenv(usecwd=True)`` (NOT the bare ``load_dotenv()``
default, which searches upward from THIS module's file and would find pux's own
repo ``.env``) so keys land in ``os.environ`` without the consumer shell-exporting
them. Pins logging to stderr when ``pin_stderr=True`` (the stdio path — stdout is
the JSON-RPC wire). Idempotent.
"""
from __future__ import annotations

import logging
import sys

from dotenv import find_dotenv, load_dotenv


def bootstrap_env_and_logging(*, pin_stderr: bool = False) -> None:
    """Load ``./.env`` (launch-CWD-anchored); optionally pin logging to stderr.

    Call as the FIRST thing an entrypoint does, before any env read. ``usecwd``
    is load-bearing — see the module docstring.
    """
    dotenv_path = find_dotenv(usecwd=True)
    if dotenv_path:
        load_dotenv(dotenv_path)
    if pin_stderr:
        logging.basicConfig(stream=sys.stderr, force=True)
