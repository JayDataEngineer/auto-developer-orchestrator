You are ingesting a Telegram chat export.

## Delegation Workflow

1. **Delegate to ingestion-director**: "Ingest the Telegram export at [path]. Parse all messages, process all media (audio transcriptions with speaker diarization, image analysis, face recognition and identity clustering), extract entities, cluster content, and build a Neo4j knowledge graph. Be thorough — process every file."

The ingestion director handles the full pipeline:
- Telegram HTML/JSON parsing via telegram_parser.py
- Audio transcription + speaker diarization via MCP tools
- Image analysis + OCR + object detection via MCP vision tools
- Face recognition via CompreFace + identity clustering via HDBSCAN
- Entity extraction (people, orgs, locations, topics, dates)
- Content clustering with Neo4j topic reuse
- Neo4j knowledge graph building

## After Ingestion
Optionally delegate to artifact-director if the user wants a specific output format (report, podcast, etc.).

Report progress and final stats to the user:
- Messages processed
- Media files processed (by type: audio, images, video)
- People identified (via face recognition + sender names)
- Identity clusters (cross-media person matching)
- Entities extracted
- Graph nodes/relationships created
