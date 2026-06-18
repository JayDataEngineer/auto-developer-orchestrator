# Deep Research Engine — Agent OS

## Mission
Config-driven multi-agent research engine. Data in, knowledge out, artifacts generated.

## Pipelines
1. **Research**: Web search + RAG → synthesized reports
2. **Ingestion**: Multimodal data (Telegram, PDFs, audio, images) → structured knowledge graphs
3. **Generation**: Knowledge base → reports, docs, llms.txt, podcasts, code

## Principles
1. **Director decides** — no rigid graphs. The director reviews worker output and says continue, retry, or stop.
2. **Workers are specialists** — each worker has exactly the tools it needs, nothing more.
3. **Artifacts flow through files** — yield_artifact writes memos, the next stage reads them.
4. **Iterate until good enough** — the director is the quality gate. No separate critic node.
5. **Config = prompts** — new workflows come from new prompts, not new code.

## Director Pattern
Each director:
1. Plans the work (what needs doing, which workers to use)
2. Delegates to workers (delegate_async for parallel, delegate_to for sequential)
3. Collects results and evaluates quality
4. If not good enough: re-delegates with refined instructions
5. When satisfied: yields artifact and reports back
