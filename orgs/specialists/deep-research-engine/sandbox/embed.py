"""Embedding helper — microsoft/harrier-oss-v1-0.6b via sentence-transformers.

This is the canonical loading path per the model card:

    from sentence_transformers import SentenceTransformer
    model = SentenceTransformer("microsoft/harrier-oss-v1-0.6b",
                                model_kwargs={"dtype": "auto"})
    q = model.encode(queries, prompt_name="web_search_query")
    d = model.encode(documents)

The model is loaded ONCE (module-level cache) and reused across calls.
First call downloads weights to $HF_HOME (~1.2GB); subsequent calls are
fast. No separate embedding server, no HTTP hop — Python in-process.

WHY TWO PROMPT MODES
--------------------
harrier is an asymmetric model: queries use the `web_search_query` prompt,
documents use the default (no prompt). Mixing them degrades retrieval.
Pass prompt_name="web_search_query" for queries; leave it None for docs.
"""
from __future__ import annotations
import os
from functools import lru_cache

MODEL_ID = os.environ.get("EMBED_MODEL_ID", "microsoft/harrier-oss-v1-0.6b")
MODEL_DTYPE = os.environ.get("EMBED_MODEL_DTYPE", "auto")


@lru_cache(maxsize=1)
def _model():
    """Lazy-load the model once. Cached so every caller shares one instance."""
    from sentence_transformers import SentenceTransformer
    m = SentenceTransformer(MODEL_ID, model_kwargs={"dtype": MODEL_DTYPE})
    return m


def encode(texts, prompt_name: str | None = None):
    """Encode a string or list of strings. Returns a list of float vectors.

    prompt_name="web_search_query" for retrieval queries; None for documents.
    The model L2-normalizes its outputs (last-token pooling + norm is baked
    in), so cosine similarity is a plain dot product.
    """
    if isinstance(texts, str):
        texts = [texts]
        single = True
    else:
        single = False
    m = _model()
    kwargs = {"prompt_name": prompt_name} if prompt_name else {}
    vecs = m.encode(texts, convert_to_numpy=True, **kwargs).tolist()
    return vecs[0] if single else vecs


def dim() -> int:
    """Return the embedding dimension (1024 for harrier-oss-v1-0.6b)."""
    m = _model()
    # sentence-transformers 5.x renamed the method; support both.
    if hasattr(m, "get_embedding_dimension"):
        return m.get_embedding_dimension()
    return m.get_sentence_embedding_dimension()


if __name__ == "__main__":
    import sys
    args = sys.argv[1:]
    if not args:
        print(__doc__)
        print(f"\nMODEL: {MODEL_ID}  dim={dim()}", flush=True)
        # Self-test: two sentences, similarity check
        q = encode("Montana extremist networks", prompt_name="web_search_query")
        d1 = encode("Flathead County city councilor discussed CPUSA organizing.")
        d2 = encode("As a general guideline, the CDC average protein requirement.")
        s1 = sum(x*y for x, y in zip(q, d1))
        s2 = sum(x*y for x, y in zip(q, d2))
        print(f"cosine(q, d1_on_topic)  = {s1:+.4f}  (expect high)")
        print(f"cosine(q, d2_off_topic) = {s2:+.4f}  (expect low)")
        print("OK")
    elif args[0] == "dim":
        print(dim())
    elif args[0] == "embed":
        # Read texts from stdin (one per line) or argv, print JSON vectors
        import json
        texts = args[1:] if len(args) > 1 else sys.stdin.read().splitlines()
        print(json.dumps(encode(texts)))
    elif args[0] == "query":
        import json
        texts = args[1:] if len(args) > 1 else sys.stdin.read().splitlines()
        print(json.dumps(encode(texts, prompt_name="web_search_query")))
