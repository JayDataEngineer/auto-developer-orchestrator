#!/usr/bin/env python3
"""Neo4j client for Pux sandbox workers.

Standalone CLI — no DRE engine dependencies. Uses neo4j driver directly.

Usage:
    python3 neo4j_client.py query --cypher "MATCH (n) RETURN n LIMIT 10"
    python3 neo4j_client.py create-nodes --input nodes.json
    python3 neo4j_client.py create-rels --input rels.json
    python3 neo4j_client.py topics [--namespace NAME]
    python3 neo4j_client.py build --clusters clusters.json --entities entities.json [--namespace NAME]
    python3 neo4j_client.py entities --name "John Smith"
    python3 neo4j_client.py stats
    python3 neo4j_client.py schema

Environment:
    NEO4J_URI       (default: bolt://localhost:37687)
    NEO4J_USER      (default: neo4j)
    NEO4J_PASSWORD   (required)
    NEO4J_DATABASE   (default: neo4j)
"""

import argparse
import json
import os
import sys
from pathlib import Path


def get_driver():
    """Create Neo4j driver from environment."""
    try:
        from neo4j import GraphDatabase
    except ImportError:
        print("ERROR: neo4j package not installed. Run: pip install neo4j", file=sys.stderr)
        sys.exit(1)

    uri = os.environ.get("NEO4J_URI", "bolt://localhost:37687")
    user = os.environ.get("NEO4J_USER", "neo4j")
    password = os.environ.get("NEO4J_PASSWORD", "")
    if not password:
        print("ERROR: NEO4J_PASSWORD not set", file=sys.stderr)
        sys.exit(1)

    return GraphDatabase.driver(uri, auth=(user, password))


def run_cypher(cypher, params=None):
    """Run a Cypher query and return results as list of dicts."""
    driver = get_driver()
    database = os.environ.get("NEO4J_DATABASE", "neo4j")
    try:
        with driver.session(database=database) as session:
            result = session.run(cypher, params or {})
            return [record.data() for record in result]
    finally:
        driver.close()


def cmd_query(args):
    """Run an arbitrary Cypher query."""
    results = run_cypher(args.cypher)
    print(json.dumps(results, indent=2, default=str))
    print(f"\n({len(results)} rows)", file=sys.stderr)


def cmd_create_nodes(args):
    """Create nodes from a JSON file.

    Input format: [{"labels": ["Person"], "properties": {"name": "John"}, "merge_on": ["name"]}]
    """
    nodes = json.loads(Path(args.input).read_text())
    driver = get_driver()
    database = os.environ.get("NEO4J_DATABASE", "neo4j")
    created = 0

    try:
        with driver.session(database=database) as session:
            for node in nodes:
                labels = ":".join(node.get("labels", ["Entity"]))
                props = node.get("properties", {})
                merge_on = node.get("merge_on", [])

                if merge_on:
                    # MERGE on specified properties
                    match_props = {k: props[k] for k in merge_on if k in props}
                    set_props = {k: v for k, v in props.items() if k not in match_props}
                    match_clause = " AND ".join(f"n.{k} = ${k}" for k in match_props)
                    set_clause = ", ".join(f"n.{k} = ${k}" for k in set_props) if set_props else ""

                    cypher = f"MERGE (n:{labels} {{{match_clause}}})"
                    if set_clause:
                        cypher += f" SET {set_clause}"

                    params = {**match_props, **set_props}
                else:
                    cypher = f"CREATE (n:{labels} $props)"
                    params = {"props": props}

                session.run(cypher, params)
                created += 1
    finally:
        driver.close()

    print(json.dumps({"status": "ok", "nodes_created": created}))


def cmd_create_rels(args):
    """Create relationships from a JSON file.

    Input format: [{"source_labels": ["Person"], "source_match": {"name": "John"},
                    "target_labels": ["Topic"], "target_match": {"name": "AI"},
                    "rel_type": "BELONGS_TO", "properties": {}}]
    """
    rels = json.loads(Path(args.input).read_text())
    driver = get_driver()
    database = os.environ.get("NEO4J_DATABASE", "neo4j")
    created = 0

    try:
        with driver.session(database=database) as session:
            for rel in rels:
                src_labels = ":".join(rel.get("source_labels", ["Entity"]))
                tgt_labels = ":".join(rel.get("target_labels", ["Entity"]))
                src_match = rel.get("source_match", {})
                tgt_match = rel.get("target_match", {})
                rel_type = rel.get("rel_type", "RELATED_TO")
                rel_props = rel.get("properties", {})

                src_where = " AND ".join(f"a.{k} = $src_{k}" for k in src_match)
                tgt_where = " AND ".join(f"b.{k} = $tgt_{k}" for k in tgt_match)

                params = {}
                for k, v in src_match.items():
                    params[f"src_{k}"] = v
                for k, v in tgt_match.items():
                    params[f"tgt_{k}"] = v

                props_str = ""
                if rel_props:
                    props_str = " " + json.dumps(rel_props)

                cypher = (
                    f"MATCH (a:{src_labels} {{{src_where}}}) "
                    f"MATCH (b:{tgt_labels} {{{tgt_where}}}) "
                    f"MERGE (a)-[r:{rel_type}{props_str}]->(b)"
                )

                session.run(cypher, params)
                created += 1
    finally:
        driver.close()

    print(json.dumps({"status": "ok", "rels_created": created}))


def cmd_topics(args):
    """List existing Topic nodes in Neo4j."""
    namespace = args.namespace or "default"
    results = run_cypher(
        "MATCH (t:Topic) WHERE t.namespace = $ns OR $ns = '__all__' "
        "RETURN t.name as name, t.summary as summary ORDER BY t.name",
        {"ns": namespace if namespace != "__all__" else "__all__"},
    )

    if not results:
        # Try without namespace filter
        results = run_cypher("MATCH (t:Topic) RETURN t.name as name, t.summary as summary ORDER BY t.name")

    for r in results:
        name = r.get("name", "")
        summary = r.get("summary", "")
        if summary:
            print(f"- {name}: {summary}")
        else:
            print(f"- {name}")

    print(f"\n({len(results)} topics)", file=sys.stderr)


def cmd_entities(args):
    """Look up entities by name."""
    results = run_cypher(
        "MATCH (e) WHERE e.name CONTAINS $name "
        "RETURN labels(e) as labels, e.name as name, properties(e) as props",
        {"name": args.name},
    )

    for r in results:
        labels = ":".join(r.get("labels", []))
        name = r.get("name", "")
        print(f"(:{labels} {{name: '{name}'}})")

    if not results:
        print(f"No entities found matching '{args.name}'", file=sys.stderr)


def cmd_stats(args):
    """Show Neo4j database statistics."""
    stats = {}

    # Node counts by label
    labels = run_cypher("CALL db.labels() YIELD label RETURN label")
    for row in labels:
        label = row["label"]
        count = run_cypher(f"MATCH (n:{label}) RETURN count(n) as count")
        stats[label] = count[0]["count"] if count else 0

    # Relationship counts
    rel_types = run_cypher("CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType")
    rel_stats = {}
    for row in rel_types:
        rt = row["relationshipType"]
        count = run_cypher(f"MATCH ()-[r:{rt}]->() RETURN count(r) as count")
        rel_stats[rt] = count[0]["count"] if count else 0

    total_nodes = sum(stats.values())
    total_rels = sum(rel_stats.values())

    print(f"Nodes: {total_nodes}")
    for label, count in sorted(stats.items(), key=lambda x: -x[1]):
        print(f"  :{label}: {count}")

    print(f"\nRelationships: {total_rels}")
    for rt, count in sorted(rel_stats.items(), key=lambda x: -x[1]):
        print(f"  -[:{rt}]->: {count}")


def cmd_schema(args):
    """Show the graph schema."""
    labels = run_cypher("CALL db.labels() YIELD label RETURN label")
    rel_types = run_cypher("CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType")

    print("Node Labels:")
    for row in labels:
        label = row["label"]
        # Get sample properties
        sample = run_cypher(f"MATCH (n:{label}) WITH n LIMIT 1 RETURN keys(n) as props")
        props = sample[0]["props"] if sample else []
        print(f"  :{label} ({', '.join(props)})")

    print("\nRelationship Types:")
    for row in rel_types:
        print(f"  -[:{row['relationshipType']}]->")

    # Show actual patterns
    patterns = run_cypher(
        "MATCH (a)-[r]->(b) WITH DISTINCT labels(a) as from_labels, type(r) as rel_type, "
        "labels(b) as to_labels RETURN from_labels, rel_type, to_labels LIMIT 50"
    )
    if patterns:
        print("\nPatterns:")
        for p in patterns:
            src = ":".join(p["from_labels"])
            tgt = ":".join(p["to_labels"])
            print(f"  (:{src})-[:{p['rel_type']}]->(:{tgt})")


def cmd_build(args):
    """Build graph from clustered content + extracted entities.

    Creates Topic nodes from clusters, Entity nodes from entities,
    and BELONGS_TO / RELATED_TOPIC relationships.
    """
    clusters = json.loads(Path(args.clusters).read_text()) if args.clusters else []
    entities_data = json.loads(Path(args.entities).read_text()) if args.entities else {}
    namespace = args.namespace or "default"

    nodes = []
    rels = []

    # Topic nodes from clusters
    for cluster in clusters:
        name = cluster.get("name", "").strip()
        if not name:
            continue
        nodes.append({
            "labels": ["Topic"],
            "properties": {
                "name": name,
                "summary": cluster.get("summary", ""),
                "namespace": namespace,
            },
            "merge_on": ["name"],
        })
        # Link key entities to topic
        for entity_name in cluster.get("key_entities", []):
            entity_name = str(entity_name).strip()
            if entity_name:
                nodes.append({"labels": ["Person"], "properties": {"name": entity_name}, "merge_on": ["name"]})
                rels.append({
                    "source_labels": ["Person"],
                    "source_match": {"name": entity_name},
                    "target_labels": ["Topic"],
                    "target_match": {"name": name},
                    "rel_type": "BELONGS_TO",
                })

    # Entity nodes from extraction results
    for person in entities_data.get("people", []):
        nodes.append({"labels": ["Person"], "properties": {"name": person}, "merge_on": ["name"]})
    for location in entities_data.get("locations", []):
        nodes.append({"labels": ["Location"], "properties": {"name": location}, "merge_on": ["name"]})
    for topic in entities_data.get("topics", []):
        nodes.append({"labels": ["Topic"], "properties": {"name": topic, "namespace": namespace}, "merge_on": ["name"]})

    # RELATED_TOPIC between co-occurring topics (share entities)
    entity_to_topics = {}
    for cluster in clusters:
        for entity in cluster.get("key_entities", []):
            entity_to_topics.setdefault(str(entity).strip(), []).append(cluster.get("name", ""))

    seen_pairs = set()
    for topics in entity_to_topics.values():
        for i, t1 in enumerate(topics):
            for t2 in topics[i + 1:]:
                pair = frozenset({t1, t2})
                if pair not in seen_pairs and t1 != t2:
                    seen_pairs.add(pair)
                    rels.append({
                        "source_labels": ["Topic"],
                        "source_match": {"name": t1},
                        "target_labels": ["Topic"],
                        "target_match": {"name": t2},
                        "rel_type": "RELATED_TOPIC",
                    })

    # Write to Neo4j
    if nodes:
        import tempfile
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump(nodes, f)
            nodes_file = f.name
        cmd_create_nodes(argparse.Namespace(input=nodes_file))
        os.unlink(nodes_file)

    if rels:
        import tempfile
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump(rels, f)
            rels_file = f.name
        cmd_create_rels(argparse.Namespace(input=rels_file))
        os.unlink(rels_file)

    print(json.dumps({"status": "ok", "nodes": len(nodes), "rels": len(rels), "namespace": namespace}))


def main():
    parser = argparse.ArgumentParser(description="Neo4j client for Pux sandbox")
    sub = parser.add_subparsers(dest="command")

    # query
    p = sub.add_parser("query", help="Run a Cypher query")
    p.add_argument("--cypher", required=True, help="Cypher query string")

    # create-nodes
    p = sub.add_parser("create-nodes", help="Create nodes from JSON file")
    p.add_argument("--input", required=True, help="JSON file with node definitions")

    # create-rels
    p = sub.add_parser("create-rels", help="Create relationships from JSON file")
    p.add_argument("--input", required=True, help="JSON file with relationship definitions")

    # topics
    p = sub.add_parser("topics", help="List existing Topic nodes")
    p.add_argument("--namespace", default=None, help="Namespace filter")

    # entities
    p = sub.add_parser("entities", help="Look up entities by name")
    p.add_argument("--name", required=True, help="Entity name to search")

    # stats
    sub.add_parser("stats", help="Show database statistics")

    # schema
    sub.add_parser("schema", help="Show graph schema")

    # build
    p = sub.add_parser("build", help="Build graph from clusters + entities")
    p.add_argument("--clusters", help="JSON file with content clusters")
    p.add_argument("--entities", help="JSON file with extracted entities")
    p.add_argument("--namespace", default=None, help="Namespace for nodes")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {
        "query": cmd_query,
        "create-nodes": cmd_create_nodes,
        "create-rels": cmd_create_rels,
        "topics": cmd_topics,
        "entities": cmd_entities,
        "stats": cmd_stats,
        "schema": cmd_schema,
        "build": cmd_build,
    }
    commands[args.command](args)


if __name__ == "__main__":
    main()
