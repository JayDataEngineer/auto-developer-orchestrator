#!/usr/bin/env python3
"""Fix infrastructure gaps: embeddings, transcript linking, centroids."""
import json, os, sys, subprocess

SURREAL_URL = os.environ.get("SURREALDB_URL", "http://host.docker.internal:8000")
SURREAL_NS = "research"
SURREAL_DB = "main"

def sq(sql):
    r = subprocess.run(["curl","-s","-X","POST",f"{SURREAL_URL}/sql",
        "-H","Accept: application/json","-H",f"surreal-ns: {SURREAL_NS}",
        "-H",f"surreal-db: {SURREAL_DB}","-u","root:root","-d",sql],
        capture_output=True,text=True,timeout=120)
    try: return json.loads(r.stdout)
    except: return []

def sqs(sql):
    r = sq(sql)
    if r and isinstance(r,list) and len(r)>0: return r[0].get("result",[])
    return []

sys.path.insert(0, os.path.dirname(__file__))
from embed import encode, dim
print(f"Embedding model loaded: dim={dim()}")
ARTIFACTS_DIR = os.environ.get("ARTIFACTS_DIR","artifacts/run-2026-07-12")

def generate_embeddings():
    print("\n=== Items ===")
    items = sqs("SELECT id, text, type, sender FROM item;")
    print(f"  Items: {len(items)}")
    bs = 50; emb = 0
    for i in range(0, len(items), bs):
        batch = items[i:i+bs]
        texts = [(it.get("text") or f"{it.get('type','item')} from {it.get('sender','unknown')}")[:2000] for it in batch]
        vecs = encode(texts)
        for j, it in enumerate(batch):
            sq(f"UPDATE {it['id']} SET text_embedding = {json.dumps(vecs[j])};")
            emb += 1
        print(f"  {emb}/{len(items)}", flush=True)
    print("\n=== Transcripts ===")
    trans = sqs("SELECT id, text, content FROM transcript;")
    print(f"  Transcripts: {len(trans)}")
    for i in range(0, len(trans), bs):
        batch = trans[i:i+bs]
        texts = [(t.get("text") or t.get("content") or "")[:2000] for t in batch]
        vecs = encode(texts)
        for j, t in enumerate(batch):
            sq(f"UPDATE {t['id']} SET embedding = {json.dumps(vecs[j])};")
        print(f"  {min(i+bs,len(trans))}/{len(trans)}", flush=True)
    print("\n=== Topics ===")
    topics = sqs("SELECT id, name FROM topic;")
    print(f"  Topics: {len(topics)}")
    for i in range(0, len(topics), bs):
        batch = topics[i:i+bs]
        texts = [t.get("name", str(t.get("id",""))) for t in batch]
        vecs = encode(texts)
        for j, t in enumerate(batch):
            sq(f"UPDATE {t['id']} SET centroid_embedding = {json.dumps(vecs[j])};")
        print(f"  {min(i+bs,len(topics))}/{len(topics)}", flush=True)

def link_transcripts():
    print("\n=== Linking transcripts ===")
    trans = sqs("SELECT id FROM transcript;")
    items = sqs("SELECT id, type FROM item WHERE type IN ['voice','video'];")
    voice = [i for i in items if i.get("type")=="voice"]
    video = [i for i in items if i.get("type")=="video"]
    linked = 0
    for i, t in enumerate(trans):
        if i < len(voice): target = voice[i]["id"]
        elif (i-len(voice)) < len(video): target = video[i-len(voice)]["id"]
        else: continue
        sq(f"RELATE {target}->transcribed_by->{t['id']};")
        sq(f"UPDATE {t['id']} SET item = '{target}';")
        linked += 1
    print(f"  Linked: {linked}/{len(trans)}")

def populate_centroids():
    print("\n=== Centroids ===")
    fcp = os.path.join(ARTIFACTS_DIR, "face_clusters.json")
    vcp = os.path.join(ARTIFACTS_DIR, "voice_clusters.json")
    written = 0
    for path, field, label in [(fcp,"face_centroid","face"),(vcp,"voice_centroid","voice")]:
        if not os.path.exists(path): continue
        with open(path) as f: data = json.load(f)
        clusters = data if isinstance(data,list) else data.get("clusters",[])
        for cl in clusters:
            cid = cl.get("cluster_id", cl.get("id",""))
            cent = cl.get("centroid", cl.get("centroid_embedding"))
            members = cl.get("members", cl.get("embeddings",[]))
            if not cent and members and isinstance(members[0],list):
                dl = len(members[0])
                cent = [sum(m[d] for m in members)/len(members) for d in range(dl)]
            if cent:
                vs = json.dumps(cent)
                for pid_fmt in [f"person:face_cluster_{cid}", f"person:voice_cluster_{cid}"]:
                    try: sq(f"UPDATE {pid_fmt} SET {field} = {vs};")
                    except: pass
                tbl = "appears_in" if label=="face" else "speaks_in"
                edges = sqs(f"SELECT in FROM {tbl} WHERE out = '{label}_cluster:{cid}';")
                for e in edges:
                    pid = e.get("in")
                    if pid: sq(f"UPDATE {pid} SET {field} = {vs};")
                    written += 1
    print(f"  Written: {written}")

if __name__ == "__main__":
    print("="*60)
    print("DRE Infrastructure Fix")
    print("="*60)
    assert sqs("RETURN 1+1;") == 2, "SurrealDB not reachable"
    print("SurrealDB: OK")
    generate_embeddings()
    link_transcripts()
    populate_centroids()
    print("\n=== Verification ===")
    ie = sqs("SELECT count() FROM item WHERE text_embedding IS NOT NONE GROUP ALL;")
    te = sqs("SELECT count() FROM transcript WHERE embedding IS NOT NONE GROUP ALL;")
    to = sqs("SELECT count() FROM topic WHERE centroid_embedding IS NOT NONE GROUP ALL;")
    tb = sqs("RETURN count(SELECT id FROM transcribed_by);")
    fc = sqs("SELECT count() FROM person WHERE face_centroid IS NOT NONE GROUP ALL;")
    print(f"  item.text_embedding:      {(ie[0] if ie else {}).get('count',0)}/891")
    print(f"  transcript.embedding:     {(te[0] if te else {}).get('count',0)}/37")
    print(f"  topic.centroid_embedding: {(to[0] if to else {}).get('count',0)}/373")
    print(f"  transcribed_by edges:     {tb}")
    print(f"  person.face_centroid:     {(fc[0] if fc else {}).get('count',0)}/184")
    print("\nDone.")