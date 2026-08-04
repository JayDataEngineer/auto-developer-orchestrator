"""Pytest config: put infra/nitter/ on sys.path so tests can do
``from src.settings import ...`` (matching the in-container layout where
CMD is ``python -m src.server`` from /app).

Also isolates the env var namespace per test — settings.py uses
``os.environ`` discovery, so tests that mutate env vars must not leak state
into siblings. The fixture below snapshots + restores env around each test.
"""
from __future__ import annotations

import os
import sys
from pathlib import Path

NITTER_ROOT = Path(__file__).resolve().parent.parent  # infra/nitter/
if str(NITTER_ROOT) not in sys.path:
    sys.path.insert(0, str(NITTER_ROOT))
