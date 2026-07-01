#!/usr/bin/env python3
"""CompreFace face recognition client for Pux sandbox workers.

Standalone CLI — face recognition via CompreFace REST API.
No DRE engine dependencies.

Usage:
    python3 face_client.py recognize --image photo.jpg
    python3 face_client.py recognize --image photo.jpg --min-similarity 0.7
    python3 face_client.py add-subject --name "John Smith" --image john_face.jpg
    python3 face_client.py list-subjects
    python3 face_client.py delete-subject --name "John Smith"
    python3 face_client.py batch-recognize --input image_list.json
    python3 face_client.py cluster-embeddings --input faces.json --output clusters.json

Environment:
    COMPREFACE_BASE_URL   (default: http://localhost:8000 — Caddy ingress)
                           Caddy routes /api/v1/* → compreface-api:8080.
                           To bypass Caddy, set this to http://localhost:8001
                           with `ports:` published directly.
    COMPREFACE_API_KEY    (required — bootstrap.sh writes this to .env.local after setup)

CompreFace is part of the deep-research-engine docker-compose.yml stack.
Run `./bootstrap.sh` to bring it up and run first-time setup automatically.
"""

import argparse
import json
import os
import sys
from pathlib import Path


def get_base_url():
    return os.environ.get("COMPREFACE_BASE_URL", "http://localhost:8000")


def get_api_key():
    key = os.environ.get("COMPREFACE_API_KEY", "")
    if not key:
        print(
            "ERROR: COMPREFACE_API_KEY not set.\n"
            "Run `./bootstrap.sh` from the repo root — it writes the API key to .env.local "
            "after first-time CompreFace setup.\n"
            "Or set it manually if you already have one: export COMPREFACE_API_KEY=<key>",
            file=sys.stderr,
        )
        sys.exit(1)
    return key


def recognize_face(image_path, min_similarity=0.8):
    """Recognize faces in an image using CompreFace API."""
    import urllib.request

    url = f"{get_base_url()}/api/v1/recognition/recognize"
    api_key = get_api_key()

    # Read image file
    with open(image_path, "rb") as f:
        image_data = f.read()

    filename = Path(image_path).name
    boundary = "----FaceClientBoundary"

    # Build multipart form data
    body = (
        f"--{boundary}\r\n"
        f"Content-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\n"
        f"Content-Type: image/jpeg\r\n\r\n"
    ).encode() + image_data + f"\r\n--{boundary}--\r\n".encode()

    req = urllib.request.Request(
        f"{url}?face_plugins=calculator",
        data=body,
        headers={
            "x-api-key": api_key,
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        },
    )

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        print(f"ERROR: CompreFace returned {e.code}: {body[:200]}", file=sys.stderr)
        return []
    except Exception as e:
        print(f"ERROR: CompreFace request failed: {e}", file=sys.stderr)
        return []

    results = []
    for face_data in data.get("result", []):
        subjects = face_data.get("subjects", [])
        subject = subjects[0]["subject"] if subjects else None
        similarity = subjects[0]["similarity"] if subjects else 0.0

        results.append({
            "name": subject or "Unknown",
            "confidence": round(similarity, 4),
            "box": face_data.get("box", {}),
            "embedding": face_data.get("embedding"),
        })

    return results


def add_subject(name, image_path):
    """Add a known face to the CompreFace database."""
    import urllib.request
    import urllib.parse

    url = f"{get_base_url()}/api/v1/recognition/faces"
    api_key = get_api_key()

    with open(image_path, "rb") as f:
        image_data = f.read()

    filename = Path(image_path).name
    boundary = "----FaceClientBoundary"

    params = urllib.parse.urlencode({"subject": name})
    body = (
        f"--{boundary}\r\n"
        f"Content-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\n"
        f"Content-Type: image/jpeg\r\n\r\n"
    ).encode() + image_data + f"\r\n--{boundary}--\r\n".encode()

    req = urllib.request.Request(
        f"{url}?{params}",
        data=body,
        headers={
            "x-api-key": api_key,
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        },
    )

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read())
            return True, result
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        return False, f"HTTP {e.code}: {body[:200]}"
    except Exception as e:
        return False, str(e)


def list_subjects():
    """List all known subjects in the CompreFace database."""
    import urllib.request

    url = f"{get_base_url()}/api/v1/recognition/faces"
    api_key = get_api_key()

    req = urllib.request.Request(
        url,
        headers={"x-api-key": api_key},
    )

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        print(f"ERROR: CompreFace returned {e.code}: {body[:200]}", file=sys.stderr)
        return []
    except Exception as e:
        print(f"ERROR: CompreFace request failed: {e}", file=sys.stderr)
        return []

    # Collect unique subjects
    subjects = {}
    for face in data.get("faces", []):
        subject = face.get("subject", "Unknown")
        if subject not in subjects:
            subjects[subject] = []
        subjects[subject].append(face.get("image_id", ""))

    return [{"name": name, "face_count": len(ids), "face_ids": ids} for name, ids in sorted(subjects.items())]


def delete_subject(name):
    """Delete a subject and all its faces.

    Pre-fetches face count first (DELETE response is just ``{"subject": name}``
    with no count), then issues DELETE /recognition/subjects/{name}?force=true.
    Returns the count of faces that were registered under this subject.
    """
    import urllib.parse
    import urllib.request

    api_key = get_api_key()
    base = get_base_url()

    # Count faces before delete (so we can report a meaningful number)
    count = 0
    list_req = urllib.request.Request(
        f"{base}/api/v1/recognition/faces",
        headers={"x-api-key": api_key},
    )
    try:
        with urllib.request.urlopen(list_req, timeout=15) as resp:
            for f in json.loads(resp.read()).get("faces", []):
                if f.get("subject") == name:
                    count += 1
    except Exception:
        # If listing fails, fall through to delete anyway
        pass

    safe = urllib.parse.quote(name, safe="")
    del_req = urllib.request.Request(
        f"{base}/api/v1/recognition/subjects/{safe}?force=true",
        headers={"x-api-key": api_key},
        method="DELETE",
    )
    try:
        with urllib.request.urlopen(del_req, timeout=15):
            return count
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        print(f"ERROR: CompreFace returned {e.code}: {body[:200]}", file=sys.stderr)
        return 0
    except Exception as e:
        print(f"ERROR: CompreFace request failed: {e}", file=sys.stderr)
        return 0


def cluster_face_embeddings(faces_data, min_cluster_size=2):
    """Cluster face embeddings using HDBSCAN or DBSCAN fallback.

    Input: list of dicts with "embedding" field (from recognize output).
    Output: list of dicts with cluster assignments.
    """
    import numpy as np

    embeddings = []
    items = []
    for face in faces_data:
        emb = face.get("embedding")
        if emb:
            embeddings.append(emb)
            items.append(face)

    if not embeddings:
        print("No embeddings found in input", file=sys.stderr)
        return []

    emb_array = np.array(embeddings)

    # Normalize for cosine similarity via euclidean on unit vectors
    norms = np.linalg.norm(emb_array, axis=1, keepdims=True)
    norms[norms == 0] = 1
    normalized = emb_array / norms

    labels = None

    # Try HDBSCAN first
    try:
        import hdbscan
        clusterer = hdbscan.HDBSCAN(
            min_cluster_size=min_cluster_size,
            metric="euclidean",
            cluster_selection_epsilon=0.3,
        )
        labels = clusterer.fit_predict(normalized)
    except ImportError:
        pass
    except Exception as e:
        print(f"HDBSCAN failed ({e}), falling back to DBSCAN", file=sys.stderr)

    # Fallback to DBSCAN
    if labels is None:
        try:
            from sklearn.cluster import DBSCAN
            dbscan = DBSCAN(eps=0.4, min_samples=min_cluster_size, metric="euclidean")
            labels = dbscan.fit_predict(normalized)
        except ImportError:
            print("ERROR: Need hdbscan or scikit-learn for clustering", file=sys.stderr)
            sys.exit(1)

    # Assign cluster labels to items
    results = []
    for i, item in enumerate(items):
        cluster_id = int(labels[i])
        results.append({
            **item,
            "cluster": cluster_id,
            "cluster_label": f"person_{cluster_id}" if cluster_id >= 0 else "unclustered",
        })

    n_clusters = len(set(labels)) - (1 if -1 in labels else 0)
    print(f"Found {n_clusters} identity clusters from {len(items)} faces", file=sys.stderr)

    return results


def cmd_recognize(args):
    """Recognize faces in an image."""
    results = recognize_face(args.image, min_similarity=args.min_similarity)

    # Filter by minimum similarity
    recognized = [r for r in results if r["confidence"] >= args.min_similarity]

    output = {
        "image": args.image,
        "faces_found": len(results),
        "faces_recognized": len(recognized),
        "results": results,
    }
    print(json.dumps(output, indent=2, default=str))
    print(f"\n({len(results)} faces, {len(recognized)} above {args.min_similarity} threshold)", file=sys.stderr)


def cmd_add_subject(args):
    """Add a known face to the database."""
    success, result = add_subject(args.name, args.image)
    if success:
        print(json.dumps({"status": "ok", "subject": args.name, "result": result}, indent=2))
        print(f"\nAdded '{args.name}' to face database", file=sys.stderr)
    else:
        print(f"ERROR: Failed to add subject: {result}", file=sys.stderr)
        sys.exit(1)


def cmd_list_subjects(args):
    """List all known subjects."""
    subjects = list_subjects()
    print(json.dumps(subjects, indent=2))
    print(f"\n({len(subjects)} subjects)", file=sys.stderr)


def cmd_delete_subject(args):
    """Delete a subject and all their faces."""
    deleted = delete_subject(args.name)
    print(json.dumps({"status": "ok", "subject": args.name, "faces_deleted": deleted}))
    print(f"\nDeleted {deleted} faces for '{args.name}'", file=sys.stderr)


def cmd_batch_recognize(args):
    """Recognize faces in multiple images."""
    image_list = json.loads(Path(args.input).read_text())

    if isinstance(image_list, list) and all(isinstance(x, str) for x in image_list):
        # Simple list of paths
        paths = image_list
    elif isinstance(image_list, list):
        paths = [item.get("path", item.get("image", "")) for item in image_list]
    else:
        print("ERROR: --input must be a JSON array of image paths", file=sys.stderr)
        sys.exit(1)

    all_results = []
    for i, path in enumerate(paths):
        if not path or not Path(path).exists():
            all_results.append({"image": path, "error": "file not found"})
            continue

        faces = recognize_face(path, min_similarity=args.min_similarity)
        all_results.append({
            "image": path,
            "faces_found": len(faces),
            "results": faces,
        })

        if (i + 1) % 10 == 0:
            print(f"Processed {i + 1}/{len(paths)}...", file=sys.stderr)

    if args.output:
        Path(args.output).write_text(json.dumps(all_results, indent=2, default=str))
        print(json.dumps({"status": "ok", "images": len(all_results), "output": args.output}))
    else:
        print(json.dumps(all_results, indent=2, default=str))

    total_faces = sum(r.get("faces_found", 0) for r in all_results)
    print(f"\n({len(all_results)} images, {total_faces} total faces)", file=sys.stderr)


def cmd_cluster(args):
    """Cluster face embeddings into identity groups."""
    data = json.loads(Path(args.input).read_text())

    # Handle both raw face arrays and wrapped results
    faces = data
    if isinstance(data, dict) and "results" in data:
        faces = data["results"]
    if isinstance(data, list) and data and isinstance(data[0], dict) and "results" in data[0]:
        # Batch recognize output — flatten
        faces = []
        for item in data:
            faces.extend(item.get("results", []))

    results = cluster_face_embeddings(faces, min_cluster_size=args.min_cluster_size)

    if args.output:
        Path(args.output).write_text(json.dumps(results, indent=2, default=str))
        print(json.dumps({"status": "ok", "faces": len(results), "output": args.output}))
    else:
        print(json.dumps(results, indent=2, default=str))


def main():
    parser = argparse.ArgumentParser(description="CompreFace face recognition client for Pux sandbox")
    sub = parser.add_subparsers(dest="command")

    p = sub.add_parser("recognize", help="Recognize faces in an image")
    p.add_argument("--image", required=True, help="Path to image file")
    p.add_argument("--min-similarity", type=float, default=0.8, help="Minimum similarity threshold")

    p = sub.add_parser("add-subject", help="Add a known face to the database")
    p.add_argument("--name", required=True, help="Person's name")
    p.add_argument("--image", required=True, help="Path to face image")

    p = sub.add_parser("list-subjects", help="List all known subjects")

    p = sub.add_parser("delete-subject", help="Delete a subject from the database")
    p.add_argument("--name", required=True, help="Subject to delete")

    p = sub.add_parser("batch-recognize", help="Recognize faces in multiple images")
    p.add_argument("--input", required=True, help="JSON file with image paths")
    p.add_argument("--output", help="Output JSON file (default: stdout)")
    p.add_argument("--min-similarity", type=float, default=0.8, help="Minimum similarity threshold")

    p = sub.add_parser("cluster", help="Cluster face embeddings into identity groups")
    p.add_argument("--input", required=True, help="JSON file with face data (from recognize)")
    p.add_argument("--output", help="Output JSON file (default: stdout)")
    p.add_argument("--min-cluster-size", type=int, default=2, help="Minimum cluster size for HDBSCAN")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {
        "recognize": cmd_recognize,
        "add-subject": cmd_add_subject,
        "list-subjects": cmd_list_subjects,
        "delete-subject": cmd_delete_subject,
        "batch-recognize": cmd_batch_recognize,
        "cluster": cmd_cluster,
    }
    commands[args.command](args)


if __name__ == "__main__":
    main()
