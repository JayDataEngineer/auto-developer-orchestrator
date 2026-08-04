---
name: deep-research
description: Deep Research Engine pipeline — multimodal ingestion (PDFs, audio diarization, face clustering, persons), a context engine (query/search), web research, fact-checking, audit quality gates, and content production (Substack articles, social posts, safety-reference dossiers). Use when running a research query, an ingest job, a fact-check, or a content-production ask.
---

# Deep Research Engine

The Deep Research Engine gathers, ingests, synthesizes, audits, and publishes
multi-modal research. This skill indexes the pipeline playbooks. Read the
`references/` file for the stage you were delegated; do not read them all
upfront.

## When to read which reference

| Task | Read |
|------|------|
| Deterministic preprocessing + LLM identity labeling (the ingest runbook) | `references/PIPELINE_RUNBOOK.md` |
| Artifacts/ directory tree, entity dossier spec, provenance format | `references/ARTIFACT_STRUCTURE.md` |
| Overall context-engine architecture (ingest → report) | `references/CONTEXT_ENGINE.md` |
| Query the context engine (vector lookup of prior ingests) | `references/CONTEXT_ENGINE_QUERY.md` |
| Hybrid search across the context engine | `references/CONTEXT_ENGINE_SEARCH.md` |
| Web research workflow (gather sources for a query) | `references/RESEARCH_WEB_WORKFLOW.md` |
| Ingest PDFs / documents | `references/INGEST_PDF_DOCUMENTS.md` |
| Ingest audio + speaker diarization | `references/INGEST_AUDIO_DIARIZATION.md` |
| Ingest + cluster faces (v1 pipeline) | `references/INGEST_FACE_CLUSTERING.md` |
| Ingest + cluster faces (v2 pipeline) | `references/INGEST_FACE_CLUSTERING_V2.md` |
| Cross-modal identity linking strategies (temporal co-occurrence, voice→sender) | `references/INGEST_MULTIMODAL_PERSONS.md` |
| Fact-check claims / articles against sources | `references/FACT_CHECK_ARTICLES.md` |
| Audit + quality gates before publish | `references/AUDIT_QUALITY_GATES.md` |
| Write Substack articles | `references/WRITE_SUBSTACK_ARTICLES.md` |
| Write social posts | `references/WRITE_SOCIAL_POSTS.md` |
| Write safety-reference / protective-intelligence dossier | `references/WRITE_SAFETY_REFERENCE.md` |

## Operating rule

Every published claim must trace to an ingested source or a fetched URL and
survive the audit gate. The synthesizer writes; the auditor gates; neither
ships without the other.

For safety-reference dossiers specifically: the clustering system is the
determiner of what's in the folder (sender attribution is banned); every error
caught gets a dated inline CORRECTION NOTE; both adversarial networks in a
crossover dataset get documented, not just the side the dataset was built to
expose.

