# CONTEXT_ENGINE_SEARCH

**Read this when answering a question that should use existing knowledge.** The context engine has vectors + graph + raw files — this skill is the "read" path.

Pair with [[CONTEXT_ENGINE_QUERY]] which covers structural queries (counts, gaps, audit checks). This skill covers **semantic search + graph traversal**.

## How to query

EVERY query runs via the `surreal_query` tool — SurrealDB's built-in
MCP server. The harness holds a persistent connection. You call it by name:

```
surreal_query(sql="<SurrealQL statement>")
```

For vector search, embed the query via the in-sandbox embed helper
(`sandbox/embed.py`, harrier-oss-v1-0.6b, 1024-dim), then pass the vector
into a cosine similarity query.

## Layer 1 — Vector search (semantic similarity)

Find records whose content is semantically similar to a query, regardless of exact keywords.

```
# Semantic search across transcripts (field defaults to "embedding")
surreal_query(sql="
  SELECT id, vector::similarity::cosine(embedding, $query_vec) AS score
  FROM transcript
  WHERE embedding != NONE
  ORDER BY score DESC LIMIT 5
")

# Other tables/fields:
# source.embedding, topic.centroid_embedding, item.text_embedding, face_appearance.embedding
```

To get `$query_vec`, call the in-sandbox embed helper from a declared tool
or a make_script. The model loads once via sentence-transformers (cached):

```python
# In a make_script or batch job inside the sandbox
from embed import encode
query_vec = encode("infiltration confession", prompt_name="web_search_query")
```

Then pass the vector into the MCP query as a parameter:
```
surreal_query(sql="
  SELECT id, vector::similarity::cosine(embedding, $query_vec) AS score
  FROM transcript WHERE embedding != NONE ORDER BY score DESC LIMIT 5
", query_vec=query_vec)
```

Scores >0.6 are usually relevant; >0.75 are strong matches.

## Layer 2 — Graph traversal (relations)

Once you have a record ID, walk the graph to find connected entities.

```
# What sources informed this topic?
surreal_query(sql="SELECT *, <-extracted_from<-source.{id, title, url, author} AS source_docs FROM topic:abc123")

# What topics + persons does this source inform?
surreal_query(sql="SELECT *, ->extracted_from->source.{id, title, url} AS linked_sources, <-extracted_from<-topic.name AS topics, <-extracted_from<-person.canonical_name AS persons FROM source:abc123")

# Everything about a person — photos, recordings, topics, sources
surreal_query(sql="SELECT canonical_name, face_count, voice_count, ->appears_in->item.{id, type, timestamp, path} AS photos, ->speaks_in->item.{id, type, timestamp} AS recordings, <-extracted_from<-source.{id, title, url} AS sources FROM person WHERE canonical_name CONTAINS 'Grady'")

# Items mentioned by a topic
surreal_query(sql="SELECT *, ->mentions->item.{id, type, text[0:80] AS text_preview, sender, timestamp} AS items FROM topic ORDER BY name")
```

## Layer 3 — Hybrid (vector + graph)

The most powerful pattern: use vector search to find a seed, then graph traversal to expand.

**Example**: "What do we know about infiltration in this dataset?"

```
# 1. Vector-search topics to find the seed
surreal_query(sql="SELECT id, vector::similarity::cosine(centroid_embedding, fn::embed('infiltration informant double agent')) AS score FROM topic WHERE centroid_embedding != NONE ORDER BY score DESC LIMIT 1")

# 2. Walk the graph from that seed (use the id from step 1)
surreal_query(sql="SELECT name, summary, ->mentions->item.{id, sender, timestamp, text[0:100] AS text_preview} AS items, <-extracted_from<-source.{id, title, url, author} AS sources, <-mentions<-person.canonical_name AS persons FROM topic:<seed_id>")
```

## Layer 4 — Find similar entities

Useful for deduplication, identity resolution, and surfacing related content.

```
# Find transcripts similar to a specific transcript
surreal_query(sql="SELECT id, text[0:80] AS preview, vector::similarity::cosine(embedding, (SELECT embedding FROM transcript:04edb62cpdj3lfojdcf2)[0].embedding) AS score FROM transcript WHERE id != transcript:04edb62cpdj3lfojdcf2 AND embedding != NONE ORDER BY score DESC LIMIT 5")

# Find similar persons (by face centroid)
surreal_query(sql="SELECT canonical_name, vector::similarity::cosine(face_centroid, (SELECT face_centroid FROM person:abc123)[0].face_centroid) AS score FROM person WHERE face_centroid != NONE ORDER BY score DESC LIMIT 5")
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

- **Empty results with no error** — usually means the table has no embeddings yet. Check counts via [[CONTEXT_ENGINE_QUERY]].
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
