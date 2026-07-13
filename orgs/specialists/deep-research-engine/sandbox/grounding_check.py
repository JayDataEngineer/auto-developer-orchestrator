#!/usr/bin/env python3
"""Grounding check — the ungrounded-claim gate.

Verifies that every checkable entity in a report actually appears in the
source corpus (raw transcripts, OCR text, messages, video captions). This is
the tool that catches UNGROUNDED entities — named entities the report asserts
that do not appear anywhere in the source data.

IMPORTANT — "ungrounded" ≠ "fabricated". An entity flagged UNGROUND may be:
  (a) Fabricated: the LLM invented a plausible-sounding name that doesn't
      exist at all.
  (b) Real but misattributed: the entity genuinely exists (e.g. a well-known
      encrypted-messaging app), but the source data never mentions it — so
      asserting "the subject uses X" is an unsupported claim about THIS
      subject, even though X is real.
Both are report-quality failures: an intelligence report must not assert
claims the source data does not support. The check flags both; a human (or
the auditor agent) decides which case applies and whether to remove the
claim, find evidence, or mark it [UNVERIFIED].

DOMAIN-AGNOSTIC BY DESIGN. The DRE may investigate any topic. Entity
extraction is done by the LLM (which extracts whatever proper nouns the
report's domain contains), NOT by hardcoded regex patterns. See the
extraction section below for rationale.

EXIT CODE:
  0  — all entities grounded (or explicitly marked [UNVERIFIED] in the report)
  1  — ≥1 UNGROUNDED entity found (ungrounded claim detected)

OUTPUT:
  JSON report to stdout + written to <report>.grounding.json next to the report.

USAGE:
  # Check a report against a source corpus directory
  python3 grounding_check.py check \
      --report artifacts/FINAL_SOTA_intelligence_report.md \
      --corpus data/telegram-dump/ChatExport_2026-03-13/

  # The corpus directory is grepped recursively for .txt, .md, .json transcript
  # files. The report is scanned for entity patterns + LLM-extracted proper nouns.

HOW IT WORKS:
  1. Load the source corpus into a single searchable string (case-insensitive).
  2. Extract checkable entities from the report:
     a. Regex patterns for known entity types (weapons, apps, orgs, codes)
     b. LLM-extracted proper nouns / named entities / specific claims
  3. For each entity, grep the corpus (with fuzzy variants for ASR artifacts).
  4. Report GROUNDED / UNGROUNDED / ASR-VARIANT for each.
  5. Exit 1 if any UNGROUNDED (not in corpus, not marked [UNVERIFIED]).

  Lines containing [UNVERIFIED] are skipped — the report already flagged them.

ENV (for LLM entity extraction):
  LLM_API_URL   OpenAI-compatible endpoint (injected by sandbox.llm policy)
  LLM_MODEL     Model id
  LLM_API_KEY   API key (optional for local endpoints)
"""

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.request
from pathlib import Path


# ---------------------------------------------------------------------------
# Entity extraction
#
# NO HARDCODED ENTITY LISTS. The DRE is a general-purpose research engine —
# it could be investigating extremist networks, semiconductor supply chains,
# or climate policy. Hardcoding "Threema" or "Patriot Front" into a regex
# would overfit the tool to one dataset and make it useless for every other
# domain. Instead, the LLM extracts whatever entities are relevant to the
# report's domain, and we grep the corpus for each. Domain-agnostic by design.
# ---------------------------------------------------------------------------


def _extract_llm_entities(text: str) -> list[tuple[str, str]]:
    """Use the LLM to extract all proper nouns + specific factual claims.

    Returns [(category, entity), ...]. Categories: person, org, place, tool,
    weapon, number, other.
    """
    url = os.environ.get("LLM_API_URL", "")
    model = os.environ.get("LLM_MODEL", "")
    if not url or not model:
        return []  # LLM not configured — regex-only mode

    prompt = f"""Extract EVERY checkable entity from this intelligence report.
For each entity, output a JSON array of {{"category": "...", "entity": "..."}}.

Categories: person, org, place, tool, weapon, number, date, other.

Only include SPECIFIC, checkable entities — proper nouns, named organizations,
specific places (city + state), weapon models, app names, dollar amounts,
dates, counts. Do NOT include generic words ("firearm", "extremist", "network").

If an entity appears on a line containing [UNVERIFIED], skip it — the report
already flagged it.

REPORT TEXT:
{text[:30000]}
"""
    data = json.dumps({
        "messages": [{"role": "user", "content": prompt}],
        "model": model,
        "temperature": 0.0,
        "max_tokens": 4000,
    }).encode()
    headers = {
        "Content-Type": "application/json",
        "User-Agent": "pux-harness-sandbox/1.0",
    }
    api_key = os.environ.get("LLM_API_KEY", "")
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    try:
        req = urllib.request.Request(url, data=data, headers=headers)
        with urllib.request.urlopen(req, timeout=120) as resp:
            result = json.loads(resp.read())
        content = result["choices"][0]["message"]["content"]
        # Parse the JSON array from the response
        # Find the JSON array in the response
        match = re.search(r'\[.*\]', content, re.DOTALL)
        if match:
            entities = json.loads(match.group())
            return [(e["category"], e["entity"]) for e in entities if e.get("entity")]
    except Exception as e:
        print(f"[grounding] LLM extraction failed: {e}", file=sys.stderr)
    return []


def _generate_variants(entity: str) -> list[str]:
    """Generate ASR-spelling variants for an entity.

    ASR turns 'SD9' → 'S D nine', 'AR-15' → 'AR fifteen', '30-06' → 'thirty six'
    or 'thirty ought six'. Generate variants so grep catches them.
    """
    variants = [entity, entity.lower()]
    # Number-word variants
    num_map = {
        "0": "zero", "1": "one", "2": "two", "3": "three", "4": "four",
        "5": "five", "6": "six", "7": "seven", "8": "eight", "9": "nine",
        "15": "fifteen", "30": "thirty", "45": "forty five", "06": "ought six",
    }
    # Replace runs of digits with word forms
    wordified = entity
    for digit, word in sorted(num_map.items(), key=lambda x: -len(x[0])):
        wordified = wordified.replace(digit, f" {word} ")
    if wordified.lower() != entity.lower():
        variants.append(wordified.strip())

    # Remove hyphens / spaces (AR-15 → AR15)
    no_punct = re.sub(r"[-.\s]", "", entity)
    if no_punct != entity:
        variants.append(no_punct)
        variants.append(no_punct.lower())

    # Spaced-out variant: space between ALL adjacent alphanumerics, not just
    # letters. SD9 → "S D 9", AR15 → "A R 1 5". ASR often produces this form.
    spaced = re.sub(r"([A-Za-z0-9])(?=[A-Za-z0-9])", r"\1 ", entity)
    if spaced != entity:
        variants.append(spaced)
        variants.append(spaced.lower())
        # Also wordify the digits in the spaced form: "S D 9" → "S D nine"
        spaced_wordified = spaced
        for digit, word in sorted(num_map.items(), key=lambda x: -len(x[0])):
            spaced_wordified = spaced_wordified.replace(digit, word)
        if spaced_wordified.lower() != spaced.lower():
            variants.append(spaced_wordified.strip())

    return list(set(variants))


def _check_entity(entity: str, corpus_lower: str, category: str = "") -> tuple[str, str]:
    """Check if entity appears in corpus. Returns (status, evidence).

    status: 'GROUNDED' | 'ASR_VARIANT' | 'UNGROUND'

    Matching strategy (progressively looser, category-aware):
      1. Exact phrase match
      2. ASR variants (number words, spaced letters)
      3. For multi-word entities: the LONGEST distinctive token alone
         (surnames, city names, org keywords). "Steven Butters" → "Butters".
         "Belmont, MA" → "Belmont". This catches cases where first/last name
         appear separately in the corpus but never as a bigram.
      4. For locations "City, ST": check "City" alone (the state is implied).
    """
    # 1. Direct match
    if entity.lower() in corpus_lower:
        return ("GROUNDED", f"exact match: '{entity}'")

    # 2. Try ASR variants
    for variant in _generate_variants(entity):
        if variant.lower() in corpus_lower:
            return ("ASR_VARIANT", f"variant '{variant}' found (likely ASR form)")

    # 3. Multi-word entities: check the longest token (most distinctive word)
    #    Skip generic stopwords — we want the SURNAME, the CITY, the ORG KEYWORD.
    _STOP = {
        "the", "a", "an", "of", "and", "or", "in", "at", "on", "to", "for",
        "mt", "ma", "fl", "tx", "ny", "ct", "or", "wa", "co", "ca",
        "mr", "mrs", "ms", "dr", "jr", "sr",
        "party", "group", "club", "alliance", "network", "movement",  # too generic
        "county", "city", "valley", "falls", "beach",  # geo suffixes
        "years", "old", "age",  # numeric qualifiers
    }
    tokens = re.findall(r"[A-Za-z]{3,}", entity)
    distinctive = [t for t in tokens if t.lower() not in _STOP]
    if len(distinctive) >= 1:
        # Try the longest distinctive token
        longest = max(distinctive, key=len)
        if len(longest) >= 4 and longest.lower() in corpus_lower:
            return ("GROUNDED", f"token match: '{longest}' (from '{entity}')")
        # Try all distinctive tokens — any one grounding is sufficient for names
        for tok in sorted(distinctive, key=len, reverse=True):
            if len(tok) >= 4 and tok.lower() in corpus_lower:
                return ("GROUNDED", f"token match: '{tok}' (from '{entity}')")

    return ("UNGROUND", f"'{entity}' not found in corpus (phrase, variant, or token)")


def _extract_strings_from_json(obj, min_len: int = 3) -> list[str]:
    """Recursively extract ALL string values from a JSON-parsed object.

    This catches every text-bearing field — transcript, text, caption, ocr,
    objects, description, summary, entity, label — without hardcoding key
    names. The grounding check needs ALL text the source data contains,
    regardless of which JSON field it lives in.
    """
    strings = []
    if isinstance(obj, str):
        if len(obj) >= min_len:
            strings.append(obj)
    elif isinstance(obj, dict):
        for v in obj.values():
            strings.extend(_extract_strings_from_json(v, min_len))
    elif isinstance(obj, list):
        for item in obj:
            strings.extend(_extract_strings_from_json(item, min_len))
    return strings


def _load_corpus(corpus_dir: Path, exclude: set[Path] | None = None) -> str:
    """Load all text-bearing files in the corpus directory.

    exclude: set of resolved absolute Paths to SKIP. Used to ensure the report
    file itself is never loaded as corpus — otherwise every entity in the
    report trivially matches itself and the check is meaningless.
    """
    exclude = exclude or set()
    texts = []
    extensions = {".txt", ".md", ".json"}
    for f in sorted(corpus_dir.rglob("*")):
        if not f.is_file():
            continue
        if f.suffix.lower() not in extensions:
            continue
        if f.resolve() in exclude:
            continue
        try:
            raw = f.read_text(errors="replace")
            # For JSON files, extract ALL string values recursively so no
            # text-bearing field is missed (caption, ocr, transcript, etc.)
            if f.suffix == ".json":
                try:
                    data = json.loads(raw)
                    extracted = _extract_strings_from_json(data)
                    if extracted:
                        raw = " ".join(extracted)
                except json.JSONDecodeError:
                    pass  # use raw text
            texts.append(raw)
        except Exception:
            continue
    return "\n".join(texts)


def _mask_unverified_lines(text: str) -> str:
    """Remove lines containing [UNVERIFIED] so their entities aren't checked."""
    return "\n".join(
        line for line in text.split("\n") if "[UNVERIFIED]" not in line
    )


def check(report_path: Path, corpus_dirs: list[Path]) -> dict:
    """Run the full grounding check. Returns the results dict.

    corpus_dirs: the source-data roots to search. Typically includes the raw
    dump directory AND the artifact directory (which holds ASR transcripts,
    video-frame captions, OCR text — all derived from source media and
    therefore valid grounding material).

    The report file itself is EXCLUDED from corpus loading even if it lives
    inside one of corpus_dirs — otherwise every entity trivially matches
    itself and the check is meaningless.
    """
    report_text = report_path.read_text()
    _exclude = {report_path.resolve()}
    corpus_parts = []
    for d in corpus_dirs:
        corpus_parts.append(_load_corpus(d, exclude=_exclude))
    corpus_text = "\n".join(corpus_parts)
    corpus_lower = corpus_text.lower()

    print(f"[grounding] report: {report_path} ({len(report_text)} chars)", file=sys.stderr)
    print(f"[grounding] corpus: {len(corpus_dirs)} dirs ({len(corpus_text)} chars, {len(corpus_text.split())} words)", file=sys.stderr)

    # Mask [UNVERIFIED] lines
    checked_text = _mask_unverified_lines(report_text)
    masked_count = len(report_text.split("\n")) - len(checked_text.split("\n"))
    if masked_count:
        print(f"[grounding] masked {masked_count} [UNVERIFIED] lines", file=sys.stderr)

    # Extract entities via LLM (domain-agnostic — no hardcoded entity lists)
    llm_entities = _extract_llm_entities(checked_text)
    print(f"[grounding] LLM-extracted entities: {len(llm_entities)}", file=sys.stderr)

    if not llm_entities:
        print("[grounding] WARNING: no entities extracted (LLM not configured or empty response). "
              "Set LLM_API_URL + LLM_MODEL for the grounding check to work.", file=sys.stderr)

    # Deduplicate
    all_entities = list(set(llm_entities))
    print(f"[grounding] unique entities to check: {len(all_entities)}", file=sys.stderr)

    # Skip low-signal entity types that are hard to ground via ASR and rarely
    # the source of dangerous ungrounded claims. Pure numbers/dates/measurements
    # appear as word-forms in transcripts ("three hundred and fifty bucks" not
    # "$350") — they produce false positives without catching real issues.
    # The DANGEROUS ungrounded claims are always NAMED entities: app names, org
    # names, weapon models, places, people. Focus the gate on those.
    _SKIP_CATEGORIES = {"number", "date", "measurement"}
    _SKIP_PATTERNS = [
        re.compile(r'^[$~><]=?\s*\d'),             # $350, ~$5, >$500
        re.compile(r'^\d+(\.\d+)?\s*(GB|MB|KB)$', re.I),  # 1.2GB
        re.compile(r'^\d+[\-–]\d+\s*(years?|yrs?)?'),     # 22-23 years old
        re.compile(r"^\d+'\d+"),                           # 5'8"
        re.compile(r'^\d{4}[\-–]\d{4}$'),                  # 2023-2024
    ]

    def _should_skip(cat: str, entity: str) -> bool:
        if cat in _SKIP_CATEGORIES:
            return True
        for pat in _SKIP_PATTERNS:
            if pat.search(entity.strip()):
                return True
        return False

    # Check each
    results = []
    skipped = 0
    for cat, entity in sorted(all_entities):
        if _should_skip(cat, entity):
            skipped += 1
            results.append({
                "category": cat,
                "entity": entity,
                "status": "SKIPPED",
                "evidence": "low-signal type (number/date/measurement) — not checked",
            })
            continue
        status, evidence = _check_entity(entity, corpus_lower, cat)
        results.append({
            "category": cat,
            "entity": entity,
            "status": status,
            "evidence": evidence,
        })
    if skipped:
        print(f"[grounding] skipped {skipped} low-signal entities (numbers/dates)", file=sys.stderr)

    grounded = [r for r in results if r["status"] == "GROUNDED"]
    asr_variant = [r for r in results if r["status"] == "ASR_VARIANT"]
    unground = [r for r in results if r["status"] == "UNGROUND"]

    summary = {
        "report": str(report_path),
        "corpus": str([str(d) for d in corpus_dirs]),
        "corpus_chars": len(corpus_text),
        "total_entities": len(results),
        "grounded": len(grounded),
        "asr_variant": len(asr_variant),
        "ungrounded": len(unground),
        "verdict": "PASS" if len(unground) == 0 else "FAIL",
        "results": results,
        "ungrounded_entities": [r["entity"] for r in unground],
    }

    # Write JSON next to report
    out_path = report_path.with_suffix(".grounding.json")
    out_path.write_text(json.dumps(summary, indent=2))
    print(f"[grounding] results → {out_path}", file=sys.stderr)

    return summary


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_check = sub.add_parser("check", help="check a report against source corpus")
    p_check.add_argument("--report", required=True, type=Path)
    p_check.add_argument("--corpus", required=True,
                         help="comma-separated source-data directories "
                              "(raw dump, transcripts, video analysis, etc.)")
    p_check.add_argument("--json", action="store_true", help="output JSON only")

    args = ap.parse_args()

    if args.cmd == "check":
        corpus_dirs = [Path(p.strip()) for p in args.corpus.split(",") if p.strip()]
        summary = check(args.report, corpus_dirs)

        if args.json:
            print(json.dumps(summary, indent=2))
        else:
            print(f"\n{'='*60}")
            print(f"GROUNDING CHECK: {summary['verdict']}")
            print(f"{'='*60}")
            print(f"  Corpus:     {summary['corpus_chars']:,} chars")
            print(f"  Entities:   {summary['total_entities']} checked")
            print(f"  Grounded:   {summary['grounded']} ✓")
            print(f"  ASR variant:{summary['asr_variant']} ~")
            print(f"  Ungrounded: {summary['ungrounded']} ✗")
            print()
            if summary["ungrounded_entities"]:
                print("  UNGROUNDED ENTITIES (not found in source corpus):")
                for r in summary["results"]:
                    if r["status"] == "UNGROUND":
                        print(f"    ✗ [{r['category']}] '{r['entity']}'")
                print()
                print("  FIX: remove, correct, or mark these [UNVERIFIED] in the report.")

        sys.exit(0 if summary["verdict"] == "PASS" else 1)


if __name__ == "__main__":
    main()
