#!/usr/bin/env python3
"""Vector search client for Pux sandbox workers.

Standalone CLI — queries Postgres/pgvector for similarity search.

Usage:
    python3 vector_search.py search --query "machine learning" --top-k 10
    python3 vector_search.py search --query "query" --top-k 5 --table documents
    python3 vector_search.py index --input chunks.json [--table documents]

Environment:
    CONTEXT_POSTGRES_URL   (default: postgresql://localhost:5433/deep_research)
    EMBEDDING_API_URL      (default: http://localhost:18080/v1/embeddings)
    EMBEDDING_MODEL        (default: jina-v5-embed)
"""

import argparse
import json
import os
import sys
from pathlib import Path


def get_connection():
    """Create psycopg2 connection from environment."""
    try:
        import psycopg2
    except ImportError:
        print("ERROR: psycopg2 not installed. Run: pip install psycopg2-binary", file=sys.stderr)
        sys.exit(1)

    url = os.environ.get("CONTEXT_POSTGRES_URL", "postgresql://localhost:5433/deep_research")
    return psycopg2.connect(url)


def get_embedding(text):
    """Get embedding vector from the embedding API."""
    import urllib.request

    api_url = os.environ.get("EMBEDDING_API_URL", "http://localhost:18080/v1/embeddings")
    model = os.environ.get("EMBEDDING_MODEL", "jina-v5-embed")

    data = json.dumps({"input": text, "model": model}).encode()
    req = urllib.request.Request(
        api_url,
        data=data,
        headers={"Content-Type": "application/json"},
    )

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read())
            return result["data"][0]["embedding"]
    except Exception as e:
        print(f"ERROR: Embedding API failed: {e}", file=sys.stderr)
        sys.exit(1)


def ensure_table(conn, table="documents"):
    """Create the vector table if it doesn't exist."""
    with conn.cursor() as cur:
        # Check if pgvector extension exists
        cur.execute("SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector')")
        has_vector = cur.fetchone()[0]

        if not has_vector:
            cur.execute("CREATE EXTENSION IF NOT EXISTS vector")
            conn.commit()

        cur.execute(f"""
            CREATE TABLE IF NOT EXISTS {table} (
                id SERIAL PRIMARY KEY,
                content TEXT,
                metadata JSONB DEFAULT '{{}}',
                embedding vector(1024),
                created_at TIMESTAMP DEFAULT NOW()
            )
        """)

        # Create index if not exists
        cur.execute(f"""
            CREATE INDEX IF NOT EXISTS {table}_embedding_idx
            ON {table} USING ivfflat (embedding vector_cosine_ops)
            WITH (lists = 100)
        """)
        conn.commit()


def cmd_search(args):
    """Search for similar documents using vector similarity."""
    query_embedding = get_embedding(args.query)
    table = args.table
    top_k = args.top_k

    conn = get_connection()
    try:
        ensure_table(conn, table)
        with conn.cursor() as cur:
            cur.execute(
                f"""
                SELECT content, metadata, 1 - (embedding <=> %s::vector) as similarity
                FROM {table}
                WHERE embedding IS NOT NULL
                ORDER BY embedding <=> %s::vector
                LIMIT %s
                """,
                (str(query_embedding), str(query_embedding), top_k),
            )
            rows = cur.fetchall()

        results = []
        for content, metadata, similarity in rows:
            results.append({
                "content": content,
                "metadata": metadata,
                "similarity": round(float(similarity), 4),
            })

        print(json.dumps(results, indent=2, default=str))
        print(f"\n({len(results)} results)", file=sys.stderr)
    finally:
        conn.close()


def cmd_index(args):
    """Index documents from a JSON file into pgvector.

    Input format: [{"content": "text", "metadata": {"source": "..."}}]
    """
    chunks = json.loads(Path(args.input).read_text())
    table = args.table

    if not chunks:
        print("No chunks to index", file=sys.stderr)
        return

    conn = get_connection()
    try:
        ensure_table(conn, table)

        indexed = 0
        with conn.cursor() as cur:
            for chunk in chunks:
                content = chunk.get("content", chunk.get("text", ""))
                if not content:
                    continue
                metadata = chunk.get("metadata", {})

                embedding = get_embedding(content)
                cur.execute(
                    f"INSERT INTO {table} (content, metadata, embedding) VALUES (%s, %s, %s::vector)",
                    (content, json.dumps(metadata), str(embedding)),
                )
                indexed += 1

                if indexed % 50 == 0:
                    conn.commit()
                    print(f"Indexed {indexed}/{len(chunks)}...", file=sys.stderr)

        conn.commit()
        print(json.dumps({"status": "ok", "indexed": indexed}))
    finally:
        conn.close()


def main():
    parser = argparse.ArgumentParser(description="Vector search for Pux sandbox")
    sub = parser.add_subparsers(dest="command")

    p = sub.add_parser("search", help="Search for similar documents")
    p.add_argument("--query", required=True, help="Search query")
    p.add_argument("--top-k", type=int, default=10, help="Number of results")
    p.add_argument("--table", default="documents", help="Table name")

    p = sub.add_parser("index", help="Index documents into pgvector")
    p.add_argument("--input", required=True, help="JSON file with document chunks")
    p.add_argument("--table", default="documents", help="Table name")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {"search": cmd_search, "index": cmd_index}
    commands[args.command](args)


if __name__ == "__main__":
    main()
