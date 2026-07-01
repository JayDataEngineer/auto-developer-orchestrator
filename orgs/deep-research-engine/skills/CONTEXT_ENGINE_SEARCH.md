# CONTEXT_ENGINE_SEARCH

**Read this when answering a question that should use existing knowledge.** The context engine has vectors + graph + raw files — this skill is the "read" path.

Pair with [[CONTEXT_ENGINE_QUERY]] which covers structural queries (counts, gaps, audit checks). This skill covers **semantic search + graph traversal**.

## Setup

```bash
export SURREAL_PASSWORD=root
URL=http://localhost:8000/surreal/sql
HDR=(-H "Accept: application/json" -H "surreal-ns: research" -H "surreal-db: main")
AUTH=-u "root:$SURREAL_PASSWORD"

# Embedding API (Ollama, OpenAI-compatible)
EMB_URL=http://localhost:11434/v1/embeddings
EMB_MODEL=mxbai-embed-large  # 1024-dim, matches schema
```

## Layer 1 — Vector search (semantic similarity)

Find records whose content is semantically similar to a query, regardless of exact keywords.

```bash
# Use the surreal_client helper (handles embedding + query in one call)
python3 /sandbox/surreal_client.py vector-search \
  --table transcript \
  --query "infiltration confession double agent" \
  --k 5

# Other tables/fields:
python3 /sandbox/surreal_client.py vector-search --table source --query "SpaceX orbital launch" --k 5
python3 /sandbox/surreal_client.py vector-search --table topic --field centroid_embedding --query "gun ownership" --k 5
python3 /sandbox/surreal_client.py vector-search --table item --field text_embedding --query "Flathead County militias" --k 5
```

Returns `[{id, score}, ...]` sorted by cosine similarity. Scores >0.6 are usually relevant; >0.75 are strong matches.

## Layer 2 — Graph traversal (relations)

Once you have a record ID, walk the graph to find connected entities.

```bash
# What sources informed this topic?
curl -sX POST $URL "${HDR[@]}" $AUTH \
  -d 'SELECT *, <-extracted_from<-source.{id, title, url, author} AS source_docs FROM topic:abc123' \
  | jq -c '.[0].result[]'

# What topics + persons does this source inform?
curl -sX POST $URL "${HDR[@]}" $AUTH \
  -d 'SELECT *, ->extracted_from->source.{id, title, url} AS linked_sources, <-extracted_from<-topic.name AS topics, <-extracted_from<-person.canonical_name AS persons FROM source:abc123' \
  | jq -c '.[0].result[]'

# Everything about a person — photos, recordings, topics, sources
curl -sX POST $URL "${HDR[@]}" $AUTH \
  -d "SELECT canonical_name, face_count, voice_count,
        ->appears_in->item.{id, type, timestamp, path} AS photos,
        ->speaks_in->item.{id, type, timestamp} AS recordings,
        <-extracted_from<-source.{id, title, url} AS sources
      FROM person WHERE canonical_name CONTAINS 'Grady'" \
  | jq -c '.[0].result[]'

# Items mentioned by a topic
curl -sX POST $URL "${HDR[@]}" $AUTH \
  -d 'SELECT *, ->mentions->item.{id, type, text_preview: (text[0:80] ?? "(media)"), sender, timestamp} AS items FROM topic ORDER BY name' \
  | jq -c '.[0].result[]'
```

## Layer 3 — Hybrid (vector + graph)

The most powerful pattern: use vector search to find a seed, then graph traversal to expand.

**Example**: "What do we know about infiltration in this dataset?"

```bash
# 1. Vector-search topics to find the seed
SEED_TOPIC=$(python3 /sandbox/surreal_client.py vector-search \
  --table topic --field centroid_embedding \
  --query "infiltration informant double agent" --k 1 \
  | jq -r '.[0].id')
echo "Seed: $SEED_TOPIC"

# 2. Walk the graph from that seed
curl -sX POST $URL "${HDR[@]}" $AUTH \
  -d "SELECT name, summary,
        ->mentions->item.{id, sender, timestamp, text_preview: (text[0:100] ?? \"(media)\")} AS items,
        <-extracted_from<-source.{id, title, url, author} AS sources,
        <-mentions<-person.canonical_name AS persons
      FROM $SEED_TOPIC" \
  | jq -c '.[0].result[]'
```

## Layer 4 — Find similar entities

Useful for deduplication, identity resolution, and surfacing related content.

```bash
# Find transcripts similar to a specific transcript
SEED_ID="transcript:04edb62cpdj3lfojdcf2"
curl -sX POST $URL "${HDR[@]}" $AUTH \
  -d "SELECT id, text[0:80] AS preview, vector::similarity::cosine(embedding, (SELECT embedding FROM $SEED_ID)[0].embedding) AS score
      FROM transcript WHERE id != $SEED_ID AND embedding != NONE
      ORDER BY score DESC LIMIT 5" \
  | jq -c '.[0].result[]'

# Find similar persons (by face centroid)
curl -sX POST $URL "${HDR[@]}" $AUTH \
  -d "SELECT canonical_name, vector::similarity::cosine(face_centroid, (SELECT face_centroid FROM person:abc123)[0].face_centroid) AS score
      FROM person WHERE face_centroid != NONE
      ORDER BY score DESC LIMIT 5" \
  | jq -c '.[0].result[]'
```

## Decision tree: which layer?

| Question shape | Layer | Example |
|---|---|---|
| "What do we know about X?" (semantic) | 1 + 2 | Vector-search for X, then graph-walk from the seed |
| "Show me everything about person Y" | 2 | Direct graph traversal from `person:Y` |
| "What sources back this claim?" | 2 | Walk `<-extracted_from<-source` from the topic/person |
| "Find similar items to Z" | 4 | Cosine similarity from Z's embedding |
| "How many X exist?" | [[CONTEXT_ENGINE_QUERY]] | Structural counts, not semantic |

## Common pitfalls

- **Empty results with no error** — usually means the table has no embeddings yet. Run `backfill_text_embeddings.py` (in `sandbox/`) or check counts via CONTEXT_ENGINE_QUERY.
- **Score < 0.3 on a "should be relevant" query** — the model may not have seen this exact phrasing. Try rephrasing with more concrete nouns.
- **`Incorrect arguments for function vector::similarity::cosine()`** — you're hitting a row without an embedding. Always filter `WHERE embedding != NONE`.
- **Time::now() vs ISO 8601** — `accessed_at` should be ISO 8601 (`2026-06-19`), not "today".

## When to use what

| Use case | Tool |
|---|---|
| Free-text query against existing knowledge | Layer 1 (vector search) |
| Provenance / source-of-truth question | Layer 2 (graph) |
| Open-ended "tell me about X" | Layer 3 (hybrid) |
| Deduplication, related-content surfacing | Layer 4 (cosine sim) |
| Counting / audit / structural | [[CONTEXT_ENGINE_QUERY]] |
