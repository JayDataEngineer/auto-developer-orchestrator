package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/base"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func RegisterAll(tools []core.Tool, db common.DBProvider) []core.Tool {
	if db == nil {
		return tools
	}
	return append(tools,
		base.New("graph_query", "Execute a Cypher query on Neo4j",
			json.RawMessage(`{"type":"object","properties":{"cypher":{"type":"string"},"params":{"type":"object"}},"required":["cypher"]}`),
			queryExec(db)),
		base.New("graph_create_nodes", "Create nodes from JSON array",
			json.RawMessage(`{"type":"object","properties":{"nodes":{"type":"array"}},"required":["nodes"]}`),
			createNodesExec(db)),
		base.New("graph_create_rels", "Create relationships between nodes",
			json.RawMessage(`{"type":"object","properties":{"relationships":{"type":"array"}},"required":["relationships"]}`),
			createRelsExec(db)),
		base.New("graph_topics", "List Topic nodes",
			json.RawMessage(`{"type":"object","properties":{"namespace":{"type":"string"}}}`),
			topicsExec(db)),
		base.New("graph_entities", "Look up entity by name",
			json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
			entitiesExec(db)),
		base.New("graph_build", "Full build from clusters and entities JSON files",
			json.RawMessage(`{"type":"object","properties":{"clusters_path":{"type":"string"},"entities_path":{"type":"string"},"namespace":{"type":"string"}},"required":["clusters_path","entities_path"]}`),
			buildExec(db)),
		base.New("graph_stats", "Get node and relationship counts",
			json.RawMessage(`{"type":"object","properties":{}}`),
			statsExec(db)),
		base.New("graph_schema", "Get graph schema info",
			json.RawMessage(`{"type":"object","properties":{}}`),
			schemaExec(db)),
		base.New("vector_search", "Semantic search using pgvector",
			json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"top_k":{"type":"integer"},"table":{"type":"string"}},"required":["query"]}`),
			vectorSearchExec(db)),
		base.New("vector_index", "Index document chunks into pgvector",
			json.RawMessage(`{"type":"object","properties":{"chunks_path":{"type":"string"},"table":{"type":"string"}},"required":["chunks_path"]}`),
			vectorIndexExec(db)),
	)
}

func queryExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		cypher, _ := base.StringArg(args, "cypher")
		if cypher == "" {
			return nil, fmt.Errorf("missing required parameter 'cypher'")
		}
		params, _ := base.MapArg(args, "params")

		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, fmt.Errorf("neo4j driver: %w", err)
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
		defer session.Close()

		result, err := session.Run(cypher, params)
		if err != nil {
			return nil, fmt.Errorf("query execution: %w", err)
		}
		records, err := result.Collect()
		if err != nil {
			return nil, fmt.Errorf("collect results: %w", err)
		}
		var output []map[string]any
		for _, rec := range records {
			row := make(map[string]any)
			for _, k := range rec.Keys {
				val, _ := rec.Get(k)
				row[k] = val
			}
			output = append(output, row)
		}
		return map[string]any{"results": output, "count": len(output)}, nil
	}
}

func createNodesExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		nodesJSON, _ := base.StringArg(args, "nodes")
		if nodesJSON == "" {
			return nil, fmt.Errorf("missing required parameter 'nodes'")
		}
		var nodes []map[string]any
		if err := json.Unmarshal([]byte(nodesJSON), &nodes); err != nil {
			return nil, fmt.Errorf("parse nodes: %w", err)
		}
		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, err
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
		defer session.Close()

		created := 0
		for _, node := range nodes {
			label, _ := node["label"].(string)
			props, _ := node["properties"].(map[string]any)
			if label == "" {
				continue
			}
			cypher := fmt.Sprintf("CREATE (n:%s $props) RETURN id(n)", label)
			_, err := session.Run(cypher, map[string]any{"props": props})
			if err == nil {
				created++
			}
		}
		return map[string]any{"created": created}, nil
	}
}

func createRelsExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		relsJSON, _ := base.StringArg(args, "relationships")
		if relsJSON == "" {
			return nil, fmt.Errorf("missing required parameter 'relationships'")
		}
		var rels []map[string]any
		if err := json.Unmarshal([]byte(relsJSON), &rels); err != nil {
			return nil, fmt.Errorf("parse relationships: %w", err)
		}
		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, err
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
		defer session.Close()

		created := 0
		for _, rel := range rels {
			from, _ := rel["from"].(string)
			to, _ := rel["to"].(string)
			relType, _ := rel["type"].(string)
			props, _ := rel["props"].(map[string]any)
			if from == "" || to == "" || relType == "" {
				continue
			}
			cypher := fmt.Sprintf("MATCH (a), (b) WHERE a.name = $from AND b.name = $to CREATE (a)-[r:%s]->(b) SET r = $props", relType)
			_, err := session.Run(cypher, map[string]any{"from": from, "to": to, "props": props})
			if err == nil {
				created++
			}
		}
		return map[string]any{"created": created}, nil
	}
}

func topicsExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		ns, _ := base.StringArg(args, "namespace")

		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, err
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
		defer session.Close()

		cypher := "MATCH (t:Topic) RETURN t.name as name, t.namespace as namespace"
		if ns != "" {
			cypher = "MATCH (t:Topic {namespace: $ns}) RETURN t.name as name, t.namespace as namespace"
		}
		result, err := session.Run(cypher, map[string]any{"ns": ns})
		if err != nil {
			return nil, fmt.Errorf("query: %w", err)
		}
		records, _ := result.Collect()
		var topics []map[string]any
		for _, rec := range records {
			name, _ := rec.Get("name")
			namespace, _ := rec.Get("namespace")
			topics = append(topics, map[string]any{"name": name, "namespace": namespace})
		}
		return map[string]any{"topics": topics, "count": len(topics)}, nil
	}
}

func entitiesExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		name, _ := base.StringArg(args, "name")
		if name == "" {
			return nil, fmt.Errorf("missing required parameter 'name'")
		}
		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, err
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
		defer session.Close()

		cypher := "MATCH (e) WHERE e.name = $name RETURN labels(e)[0] as label, properties(e) as props"
		result, err := session.Run(cypher, map[string]any{"name": name})
		if err != nil {
			return nil, err
		}
		records, _ := result.Collect()
		if len(records) == 0 {
			return map[string]any{"found": false}, nil
		}
		rec := records[0]
		label, _ := rec.Get("label")
		props, _ := rec.Get("props")
		return map[string]any{"found": true, "label": label, "properties": props}, nil
	}
}

func buildExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		clustersPath, _ := base.StringArg(args, "clusters_path")
		entitiesPath, _ := base.StringArg(args, "entities_path")
		ns, _ := base.StringArg(args, "namespace")

		if clustersPath == "" || entitiesPath == "" {
			return nil, fmt.Errorf("missing required parameters")
		}
		clustersData, err := os.ReadFile(clustersPath)
		if err != nil {
			return nil, fmt.Errorf("read clusters: %w", err)
		}
		entitiesData, err := os.ReadFile(entitiesPath)
		if err != nil {
			return nil, fmt.Errorf("read entities: %w", err)
		}
		var clusters []map[string]any
		if err := json.Unmarshal(clustersData, &clusters); err != nil {
			return nil, fmt.Errorf("parse clusters: %w", err)
		}
		var entities []map[string]any
		if err := json.Unmarshal(entitiesData, &entities); err != nil {
			return nil, fmt.Errorf("parse entities: %w", err)
		}
		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, err
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
		defer session.Close()

		createdTopics := 0
		createdEntities := 0

		for _, cluster := range clusters {
			topicName, _ := cluster["name"].(string)
			if topicName == "" {
				continue
			}
			cypher := "CREATE (t:Topic {name: $name, namespace: $ns}) RETURN id(t)"
			_, err := session.Run(cypher, map[string]any{"name": topicName, "ns": ns})
			if err == nil {
				createdTopics++
			}
		}
		for _, entity := range entities {
			entityName, _ := entity["name"].(string)
			entityType, _ := entity["type"].(string)
			if entityName == "" {
				continue
			}
			cypher := fmt.Sprintf("CREATE (e:Entity {name: $name, type: $type}) RETURN id(e)")
			_, err := session.Run(cypher, map[string]any{"name": entityName, "type": entityType})
			if err == nil {
				createdEntities++
			}
		}
		return map[string]any{"topics": createdTopics, "entities": createdEntities}, nil
	}
}

func statsExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, err
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
		defer session.Close()

		nodeResult, _ := session.Run("MATCH (n) RETURN count(n) as count", nil)
		relResult, _ := session.Run("MATCH ()-[r]->() RETURN count(r) as count", nil)

		nodeRecords, _ := nodeResult.Collect()
		relRecords, _ := relResult.Collect()

		nodes := 0
		rels := 0
		if len(nodeRecords) > 0 {
			if n, ok := nodeRecords[0].Get("count"); ok {
				if c, ok := n.(int64); ok {
					nodes = int(c)
				}
			}
		}
		if len(relRecords) > 0 {
			if r, ok := relRecords[0].Get("count"); ok {
				if c, ok := r.(int64); ok {
					rels = int(c)
				}
			}
		}
		return map[string]any{"nodes": nodes, "relationships": rels}, nil
	}
}

func schemaExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		driver, err := db.Neo4jDriver()
		if err != nil {
			return nil, err
		}
		session := driver.NewSession(neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
		defer session.Close()

		labelsResult, _ := session.Run("CALL db.labels() YIELD label RETURN label", nil)
		relsResult, _ := session.Run("CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType", nil)

		labelRecords, _ := labelsResult.Collect()
		relRecords, _ := relsResult.Collect()

		var labels []string
		for _, rec := range labelRecords {
			if l, ok := rec.Get("label"); ok {
				if s, ok := l.(string); ok {
					labels = append(labels, s)
				}
			}
		}
		var relTypes []string
		for _, rec := range relRecords {
			if r, ok := rec.Get("relationshipType"); ok {
				if s, ok := r.(string); ok {
					relTypes = append(relTypes, s)
				}
			}
		}
		return map[string]any{"labels": labels, "relationship_types": relTypes}, nil
	}
}

func vectorSearchExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		query, _ := base.StringArg(args, "query")
		topK, _ := base.IntArg(args, "top_k")
		if topK == 0 {
			topK = 5
		}
		table := base.StringArgDefault(args, "table", "documents")

		if query == "" {
			return nil, fmt.Errorf("missing required parameter 'query'")
		}
		pool, err := db.PostgresPool()
		if err != nil {
			return nil, err
		}
		embedding := make([]float32, 1536)

		var results []map[string]any
		sql := fmt.Sprintf("SELECT * FROM %s ORDER BY embedding <=> $1 LIMIT $2", table)
		rows, err := pool.Query(ctx, sql, embedding, topK)
		if err != nil {
			return nil, fmt.Errorf("query: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var id int64
			var content string
			var embedding []float32
			if err := rows.Scan(&id, &content, &embedding); err != nil {
				continue
			}
			results = append(results, map[string]any{"id": id, "content": content, "embedding": embedding})
		}
		return map[string]any{"results": results, "count": len(results)}, nil
	}
}

func vectorIndexExec(db common.DBProvider) base.ToolFunc {
	return func(ctx context.Context, args map[string]any) (any, error) {
		chunksPath, _ := base.StringArg(args, "chunks_path")
		table := base.StringArgDefault(args, "table", "documents")

		if chunksPath == "" {
			return nil, fmt.Errorf("missing required parameter 'chunks_path'")
		}
		data, err := os.ReadFile(chunksPath)
		if err != nil {
			return nil, fmt.Errorf("read chunks: %w", err)
		}
		var chunks []map[string]any
		if err := json.Unmarshal(data, &chunks); err != nil {
			return nil, fmt.Errorf("parse chunks: %w", err)
		}
		pool, err := db.PostgresPool()
		if err != nil {
			return nil, err
		}
		indexed := 0
		for _, chunk := range chunks {
			content, _ := chunk["content"].(string)
			if content == "" {
				continue
			}
			embedding := make([]float32, 1536)
			sql := fmt.Sprintf("INSERT INTO %s (content, embedding) VALUES ($1, $2)", table)
			_, err := pool.Exec(ctx, sql, content, embedding)
			if err == nil {
				indexed++
			}
		}
		return map[string]any{"indexed": indexed}, nil
	}
}