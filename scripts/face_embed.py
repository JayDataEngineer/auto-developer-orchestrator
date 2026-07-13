#!/usr/bin/env python3
"""Face embedding pipeline via media-mcp InsightFace (embed_faces tool).

Serves the photos dir over temp HTTP, calls embed_faces for every unique
non-thumb photo, collects 512-d MobileFaceNet embeddings.

Outputs:
  artifacts/<run>/face_embeddings.json  - raw {photo: [embeddings]}
  artifacts/<run>/face_analysis.json    - per-photo face records (detect + embed)
"""

import http.server
import json
import os
import socketserver
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request
from pathlib import Path

MCP_URL = os.environ.get("MEDIA_MCP_URL", "http://host.docker.internal:8101").rstrip("/")
PHOTOS_DIR = Path(os.environ.get("PHOTOS_DIR", "/sandbox/workspace/data/telegram-dump/ChatExport_2026-03-13/photos"))
RUN_DIR = Path(os.environ.get("RUN_DIR", "/sandbox/workspace/orgs/specialists/deep-research-engine/artifacts/run-2026-07-12"))
RUN_DIR.mkdir(parents=True, exist_ok=True)

EMB_OUT = RUN_DIR / "face_embeddings.json"
ANALYSIS_OUT = RUN_DIR / "face_analysis.json"


class _QuietHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *a, **k):
        pass


def _detect_ip():
    """Return IP reachable from the MCP container."""
    try:
        r = subprocess.run(["hostname", "-I"], capture_output=True, text=True, timeout=2)
        for ip in r.stdout.split():
            if ip.startswith(("172.", "10.", "100.")):
                return ip
    except Exception:
        pass
    return "host.docker.internal"


def start_server(serve_dir):
    serve_dir = Path(serve_dir).resolve()
    cwd = os.getcwd()
    os.chdir(str(serve_dir))
    httpd = socketserver.TCPServer(("0.0.0.0", 0), _QuietHandler)
    port = httpd.server_address[1]
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    os.chdir(cwd)
    return httpd, thread, port


def mcp_call(tool_name, arguments, timeout=120):
    """Call a media-mcp tool via JSON-RPC over HTTP. Returns parsed result."""
    import requests as _requests

    url = MCP_URL + "/mcp"
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {"name": tool_name, "arguments": arguments},
    }
    resp = _requests.post(
        url, json=payload,
        headers={"Accept": "application/json, text/event-stream"},
        timeout=timeout,
    )
    raw = resp.text

    # Parse SSE response
    for line in raw.split("\n"):
        if line.startswith("data: "):
            data = json.loads(line[6:])
            if "result" in data and "content" in data["result"]:
                for content in data["result"]["content"]:
                    if content.get("type") == "text":
                        return json.loads(content["text"])
            elif "error" in data:
                raise RuntimeError("MCP error: " + str(data["error"]))
    try:
        return json.loads(raw)
    except Exception:
        raise RuntimeError("Could not parse MCP response: " + raw[:500])


def get_photo_list():
    """Return sorted list of unique non-thumb photo filenames."""
    files = []
    for f in sorted(PHOTOS_DIR.iterdir()):
        if f.is_file() and f.suffix.lower() in (".jpg", ".jpeg", ".png") and "_thumb" not in f.name:
            files.append(f.name)
    return files


def main():
    photos = get_photo_list()
    print(f"Found {len(photos)} unique non-thumb photos")

    if EMB_OUT.exists():
        existing = json.load(open(EMB_OUT))
        print(f"Existing embeddings: {len(existing)} photos. Resuming...")
    else:
        existing = {}

    ip = _detect_ip()
    print(f"Using IP: {ip}")

    httpd, thread, port = start_server(PHOTOS_DIR)
    print(f"HTTP server on port {port}")

    face_embeddings = existing
    face_analysis = json.load(open(ANALYSIS_OUT)) if ANALYSIS_OUT.exists() else []
    analyzed_names = {e.get("photo") for e in face_analysis}

    total = len(photos)
    done = len(face_embeddings)
    errors = []

    for i, photo in enumerate(photos):
        if photo in face_embeddings:
            continue

        url = f"http://{ip}:{port}/{urllib.parse.quote(photo)}"
        try:
            result = mcp_call("embed_faces", {"imageSource": url, "max_faces": 50})
            faces = result if isinstance(result, list) else result.get("faces", [])

            embeddings = []
            analysis_entry = {"photo": photo, "faces": [], "num_faces": 0}

            for face in faces:
                emb = face.get("embedding") or face.get("embedding_vector")
                bbox = face.get("bbox") or face.get("box") or {}
                score = face.get("det_score") or face.get("score") or face.get("confidence", 0)

                if emb:
                    embeddings.append(emb)
                analysis_entry["faces"].append({
                    "bbox": bbox,
                    "score": score,
                    "has_embedding": bool(emb),
                })

            analysis_entry["num_faces"] = len(embeddings)
            face_embeddings[photo] = embeddings

            if photo not in analyzed_names:
                face_analysis.append(analysis_entry)
                analyzed_names.add(photo)
            else:
                for idx, e in enumerate(face_analysis):
                    if e.get("photo") == photo:
                        face_analysis[idx] = analysis_entry
                        break

            done = len(face_embeddings)
            if done % 20 == 0 or done == total:
                print(f"  [{done}/{total}] {photo}: {len(embeddings)} faces")
                EMB_OUT.write_text(json.dumps(face_embeddings, ensure_ascii=False))
                ANALYSIS_OUT.write_text(json.dumps(face_analysis, ensure_ascii=False))

        except Exception as e:
            errors.append((photo, str(e)))
            print(f"  ERROR [{photo}]: {e}")
            time.sleep(1)

    EMB_OUT.write_text(json.dumps(face_embeddings, ensure_ascii=False))
    ANALYSIS_OUT.write_text(json.dumps(face_analysis, ensure_ascii=False))

    httpd.shutdown()
    httpd.server_close()
    thread.join(timeout=2)

    total_photos = len(face_embeddings)
    total_faces = sum(len(v) for v in face_embeddings.values())
    photos_with_faces = sum(1 for v in face_embeddings.values() if v)

    print(f"\nDONE: {total_photos} photos analyzed, {total_faces} total faces, {photos_with_faces} photos with faces")
    if errors:
        print(f"Errors: {len(errors)}")
        for photo, err in errors[:10]:
            print(f"  {photo}: {err[:100]}")


if __name__ == "__main__":
    main()
