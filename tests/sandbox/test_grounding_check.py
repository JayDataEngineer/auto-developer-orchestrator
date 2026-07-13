"""Tests for the DRE grounding check — the ungrounded-claim gate.

The grounding check flags named entities in a report that do not appear in
the source corpus. These tests prove the check:
  - PASSES reports where every entity is grounded in the corpus
  - FAILS reports containing entities absent from the corpus (whether
    fabricated OR real-but-misattributed — a real entity the source data
    never mentions is still an unsupported claim about THIS subject)
  - SKIPS entities on [UNVERIFIED] lines
  - works across ARBITRARY domains: fixtures use a semiconductor supply-chain
    topic, NOT the topic the tool was originally built for, proving the tool
    is domain-agnostic (no hardcoded entity lists)

Entity extraction is LLM-based in production. Tests inject a deterministic
text-aware stand-in via monkeypatch so the full check pipeline runs without
network.
"""

import json
import sys
from pathlib import Path

import pytest

# Add the sandbox dir to path so we can import grounding_check
SANDBOX = Path(__file__).resolve().parents[2] / "orgs/specialists/deep-research-engine/sandbox"
sys.path.insert(0, str(SANDBOX))

import grounding_check


# ---------------------------------------------------------------------------
# Fixtures — SEMICONDUCTOR SUPPLY-CHAIN domain.
# Deliberately NOT the extremist-network topic the tool was originally built
# for. If the check only worked on one domain, these fixtures would fail —
# which is the point.
# ---------------------------------------------------------------------------

# Source corpus: what the raw data actually contains
CORPUS_TEXT = """
TSMC is the primary foundry for the 3nm process node.
ASML provides the EUV lithography equipment used in Hsinchu.
Dr. Elena Vasquez leads the packaging research team.
Revenue declined 12 percent year over year.
"""

# A report whose every named entity appears in the corpus → PASS
CLEAN_REPORT = """\
# Semiconductor Supply-Chain Brief

## Key Finding
TSMC produces the 3nm process node [Doc-1].
ASML EUV lithography equipment is deployed at the Hsinchu fab [Doc-2].
Dr. Elena Vasquez leads the packaging research team [Doc-3].
"""

# A report asserting entities NOT in the corpus → FAIL.
# Includes BOTH a fabricated name (Zorblax Industries) AND a real company
# absent from this corpus (Samsung is real, but this source data never
# mentions it — asserting claims about it here is ungrounded).
UNGROUND_REPORT = """\
# Semiconductor Supply-Chain Brief

## Key Finding
TSMC produces the 3nm process node [Doc-1].
Samsung is undercutting TSMC on price [Doc-2].
Zorblax Industries dominates the EUV market [Doc-3].
"""

# A report where ungrounded entities are explicitly marked [UNVERIFIED]
# → those lines are masked → those entities are not checked → PASS
UNVERIFIED_REPORT = """\
# Semiconductor Supply-Chain Brief

## Key Finding
TSMC produces the 3nm process node [Doc-1].
Samsung is undercutting TSMC on price [UNVERIFIED].
"""


@pytest.fixture
def corpus_dir(tmp_path):
    (tmp_path / "research_notes.txt").write_text(CORPUS_TEXT)
    (tmp_path / "items.json").write_text(json.dumps({
        "items": [
            {"type": "article", "text": "Vasquez presented at the Hsinchu conference"},
            {"type": "transcript", "transcript": "EUV lithography costs are rising"},
        ]
    }))
    return tmp_path


@pytest.fixture
def corpus_with_json(tmp_path):
    """Corpus with nested JSON containing varied string fields."""
    (tmp_path / "analysis.json").write_text(json.dumps([
        {"frame": "slide_001.png", "caption": "TSMC market share chart"},
        {"frame": "slide_002.png", "ocr": "ASML Holding NV"},
    ]))
    return tmp_path


def _patch_extractor(monkeypatch, entity_map):
    """Replace the LLM entity extractor with a deterministic stand-in.

    entity_map: {entity_string: category}. The stand-in returns only the
    entities that actually appear in the (already [UNVERIFIED]-masked) text
    it receives — mirroring how a real LLM extractor would only see entities
    in the text. This lets integration tests run hermetically (no network)
    while exercising the full masking → matching → verdict pipeline.
    """
    def fake_extract(text):
        lower = text.lower()
        return [(cat, ent) for ent, cat in entity_map.items()
                if ent.lower() in lower]
    monkeypatch.setattr(grounding_check, "_extract_llm_entities", fake_extract)


# ---------------------------------------------------------------------------
# LLM entity extraction
# ---------------------------------------------------------------------------

class TestExtractLlmEntities:
    def test_returns_empty_when_no_llm_configured(self, monkeypatch):
        """Without LLM_API_URL / LLM_MODEL, extraction returns [] gracefully
        rather than crashing. The check then surfaces a warning."""
        monkeypatch.delenv("LLM_API_URL", raising=False)
        monkeypatch.delenv("LLM_MODEL", raising=False)
        result = grounding_check._extract_llm_entities("Vasquez uses TSMC")
        assert result == []

    def test_returns_empty_on_network_error(self, monkeypatch):
        """If the LLM endpoint is unreachable, extraction degrades to []
        rather than raising. The caller (check()) warns and continues."""
        monkeypatch.setenv("LLM_API_URL", "http://127.0.0.1:1/nope")
        monkeypatch.setenv("LLM_MODEL", "test-model")
        result = grounding_check._extract_llm_entities("Vasquez")
        assert result == []


# ---------------------------------------------------------------------------
# ASR variant generation (domain-agnostic string transforms)
# ---------------------------------------------------------------------------

class TestGenerateVariants:
    def test_alphanumeric_spacing(self):
        # SD9 → "S D 9" (ASR often spaces out alphanumerics)
        variants = grounding_check._generate_variants("SD9")
        lower = [v.lower() for v in variants]
        assert "s d 9" in lower

    def test_digit_wordification(self):
        # SD9 → "S D nine" (ASR reads digits as words)
        variants = grounding_check._generate_variants("SD9")
        assert any("nine" in v.lower() for v in variants)

    def test_punctuation_stripped(self):
        # AR-15 → AR15 (hyphen removed)
        variants = grounding_check._generate_variants("AR-15")
        lower = [v.lower() for v in variants]
        assert "ar15" in lower


# ---------------------------------------------------------------------------
# Entity checking (matching logic — works on arbitrary strings)
# ---------------------------------------------------------------------------

class TestCheckEntity:
    def test_exact_match_grounded(self):
        status, _ = grounding_check._check_entity("TSMC", "tsmc is a foundry", "org")
        assert status == "GROUNDED"

    def test_asr_variant_match(self):
        status, _ = grounding_check._check_entity("SD9", "owns an s d nine", "weapon")
        assert status in ("GROUNDED", "ASR_VARIANT")

    def test_token_match_person_name(self):
        # Full name not in corpus as a bigram, but surname is → token match
        status, _ = grounding_check._check_entity(
            "Elena Vasquez", "vasquez presented data", "person")
        assert status == "GROUNDED"

    def test_absent_entity_unground(self):
        status, _ = grounding_check._check_entity("Zorblax", "nothing relevant here", "org")
        assert status == "UNGROUND"

    def test_real_entity_absent_from_corpus_unground(self):
        """A real entity (Samsung) NOT in this corpus is UNGROUND.
        The check tests presence in source data, not real-world existence —
        this is the 'real but misattributed' case the gate must catch."""
        status, _ = grounding_check._check_entity(
            "Samsung", "tsmc and asml mentioned", "org")
        assert status == "UNGROUND"


# ---------------------------------------------------------------------------
# [UNVERIFIED] masking
# ---------------------------------------------------------------------------

class TestMaskUnverified:
    def test_masks_unverified_lines(self):
        text = "Line 1\nUnsupported claim [UNVERIFIED]\nLine 3"
        masked = grounding_check._mask_unverified_lines(text)
        assert "Unsupported" not in masked
        assert "Line 1" in masked
        assert "Line 3" in masked

    def test_keeps_normal_lines(self):
        text = "Normal claim\nAnother claim"
        masked = grounding_check._mask_unverified_lines(text)
        assert "Normal claim" in masked
        assert "Another claim" in masked


# ---------------------------------------------------------------------------
# JSON corpus loading (recursive string extraction)
# ---------------------------------------------------------------------------

class TestLoadCorpus:
    def test_extracts_nested_json_strings(self, corpus_with_json):
        """The JSON loader must extract ALL string fields recursively —
        not just hardcoded key names. Captions, OCR text, labels in any
        field must be found by the grounding check."""
        text = grounding_check._load_corpus(corpus_with_json)
        assert "tsmc" in text.lower()
        assert "asml" in text.lower()

    def test_extracts_plain_text(self, corpus_dir):
        text = grounding_check._load_corpus(corpus_dir)
        assert "tsmc" in text.lower()
        assert "hsinchu" in text.lower()

    def test_extracts_json_string_values_not_just_keys(self, corpus_dir):
        """String VALUES (not key names) are what the corpus is searched on."""
        text = grounding_check._load_corpus(corpus_dir)
        # 'vasquez' is a value inside items.json, not a key
        assert "vasquez" in text.lower()


# ---------------------------------------------------------------------------
# Full check integration
# ---------------------------------------------------------------------------

class TestFullCheck:
    def test_clean_report_passes(self, corpus_dir, tmp_path, monkeypatch):
        """A report where every entity is in the corpus → PASS."""
        report = tmp_path / "report.md"
        report.write_text(CLEAN_REPORT)
        _patch_extractor(monkeypatch, {
            "TSMC": "org",
            "ASML": "org",
            "Hsinchu": "place",
            "Elena Vasquez": "person",
        })
        summary = grounding_check.check(report, [corpus_dir])
        assert summary["verdict"] == "PASS"
        assert summary["ungrounded"] == 0

    def test_ungrounded_report_fails(self, corpus_dir, tmp_path, monkeypatch):
        """A report asserting entities absent from the corpus → FAIL.
        Includes a fabricated name AND a real company absent from this corpus
        (the 'real but misattributed' case)."""
        report = tmp_path / "report.md"
        report.write_text(UNGROUND_REPORT)
        _patch_extractor(monkeypatch, {
            "TSMC": "org",
            "Samsung": "org",
            "Zorblax Industries": "org",
        })
        summary = grounding_check.check(report, [corpus_dir])
        assert summary["verdict"] == "FAIL"
        assert summary["ungrounded"] >= 2
        ungrounded_lower = [e.lower() for e in summary["ungrounded_entities"]]
        assert "samsung" in ungrounded_lower
        assert "zorblax industries" in ungrounded_lower

    def test_unverified_lines_skipped(self, corpus_dir, tmp_path, monkeypatch):
        """Entities on [UNVERIFIED] lines are masked → not seen by the
        extractor → not checked → not counted as ungrounded. A report whose
        only ungrounded entities are [UNVERIFIED]-marked should PASS."""
        report = tmp_path / "report.md"
        report.write_text(UNVERIFIED_REPORT)
        _patch_extractor(monkeypatch, {
            "TSMC": "org",
            "Samsung": "org",
        })
        summary = grounding_check.check(report, [corpus_dir])
        # Samsung is on a [UNVERIFIED] line → masked → extractor never
        # sees it → only TSMC checked → grounded → PASS
        assert summary["verdict"] == "PASS"
        ungrounded_lower = [e.lower() for e in summary["ungrounded_entities"]]
        assert "samsung" not in ungrounded_lower

    def test_no_entities_extracted_is_trivial_pass(self, corpus_dir, tmp_path, monkeypatch):
        """If extraction returns [] (e.g. LLM not configured), the check
        trivially PASSes (0 entities, 0 ungrounded). The check() caller
        surfaces a warning in this case. This documents that the gate is only
        meaningful when entity extraction actually runs."""
        report = tmp_path / "report.md"
        report.write_text(CLEAN_REPORT)
        _patch_extractor(monkeypatch, {})
        summary = grounding_check.check(report, [corpus_dir])
        assert summary["verdict"] == "PASS"
        assert summary["total_entities"] == 0

    def test_report_file_excluded_from_corpus(self, tmp_path, monkeypatch):
        """REGRESSION: if the report file lives inside a corpus_dir, the check
        must NOT load it as corpus. Otherwise every entity in the report
        trivially matches itself and ungrounded entities sail through as
        GROUNDED — making the entire check meaningless. This was a real bug:
        the report and corpus shared a directory and everything 'passed'."""
        # Put BOTH the source data AND the report in the same dir
        (tmp_path / "source.txt").write_text("TSMC makes chips in Hsinchu.")
        report = tmp_path / "report.md"
        report.write_text(UNGROUND_REPORT)  # contains Samsung + Zorblax
        _patch_extractor(monkeypatch, {
            "TSMC": "org",
            "Samsung": "org",
            "Zorblax Industries": "org",
        })
        summary = grounding_check.check(report, [tmp_path])
        # Despite report.md being in tmp_path, it must be excluded — Samsung
        # and Zorblax are NOT in source.txt → must be UNGROUND → FAIL
        assert summary["verdict"] == "FAIL"
        ungrounded_lower = [e.lower() for e in summary["ungrounded_entities"]]
        assert "samsung" in ungrounded_lower
        assert "zorblax industries" in ungrounded_lower
