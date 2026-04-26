package llama

func init() {
	allTools = append(allTools,
		ToolSpec{
			Name:             "delegate_to",
			Category:         CategoryOrchestration,
			Description:      "spawn a focused sub-agent with custom instructions and selected tools",
			Schema:           `{"task": "Search for Raspberry Pi prices", "instructions": "You are a price researcher. Search each store, record prices, summarize in a table.", "tools": ["mcp_call", "scrape", "search_web"]}`,
			Returns:          `Returns the sub-agent's output as an artifact. The sub-agent runs in an isolated context with only the tools you specify.`,
			ParametersSchema: `{"type":"object","properties":{"task":{"type":"string","description":"What the sub-agent should accomplish"},"instructions":{"type":"string","description":"Custom system prompt for this sub-agent. Write focused instructions that tell the sub-agent exactly how to approach the task."},"tools":{"type":"array","items":{"type":"string"},"description":"Tool names to give the sub-agent"},"max_rounds":{"type":"integer","description":"Max tool rounds (default: 15)"},"thinking_budget":{"type":"integer","description":"Max thinking tokens per turn (default: 2048)"},"temperature":{"type":"number","description":"Temperature for generation (default: 0.4)"}},"required":["task","instructions","tools"]}`,
		},
		ToolSpec{
			Name:             "delegate_async",
			Category:         CategoryOrchestration,
			Description:      "launch a sub-agent task in the background (returns immediately)",
			Schema:           `{"task": "Search for prices", "instructions": "You are a price researcher...", "tools": ["mcp_call", "scrape"], "task_id": "price-search"}`,
			Returns:          `Returns task_id immediately. Call collect_results to wait for all async tasks.`,
			ParametersSchema: `{"type":"object","properties":{"task":{"type":"string","description":"What the sub-agent should accomplish"},"instructions":{"type":"string","description":"Custom system prompt for this sub-agent"},"tools":{"type":"array","items":{"type":"string"},"description":"Tool names to give the sub-agent"},"task_id":{"type":"string","description":"Unique identifier for this async task"},"max_rounds":{"type":"integer","description":"Max tool rounds (default: 15)"},"thinking_budget":{"type":"integer","description":"Max thinking tokens per turn (default: 2048)"},"temperature":{"type":"number","description":"Temperature for generation (default: 0.4)"}},"required":["task","instructions","tools","task_id"]}`,
		},
		ToolSpec{
			Name:             "collect_results",
			Category:         CategoryOrchestration,
			Description:      "wait for all async delegates to complete and collect their results",
			Schema:           `{}`,
			Returns:          `Returns map of task_id → result for all pending async delegates.`,
			ParametersSchema: `{"type":"object","properties":{}}`,
		},
		ToolSpec{
			Name:             "create_plan",
			Category:         CategoryOrchestration,
			Description:      "create a step-by-step plan (pauses for user approval when plan-approval mode is on)",
			Schema:           `{"steps": ["Step 1 description", "Step 2 description"]}`,
			ParametersSchema: `{"type":"object","properties":{"steps":{"type":"array","items":{"type":"string"},"description":"List of step descriptions"}},"required":["steps"]}`,
		},
		ToolSpec{
			Name:             "update_plan",
			Category:         CategoryOrchestration,
			Description:      "mark a step as done/failed, or flag discovered work",
			Schema:           `{"step_index": 0, "status": "done", "note": "Found: price is $45", "discovered": false}`,
			ParametersSchema: `{"type":"object","properties":{"step_index":{"type":"integer","description":"Step index to update"},"status":{"type":"string","description":"New status: done, failed, or pending"},"note":{"type":"string","description":"Optional note about the result"},"discovered":{"type":"boolean","description":"Set true when new unlisted work is found (triggers approval in plan-approval mode)"}},"required":["step_index","status"]}`,
		},
		ToolSpec{
			Name:             "clarify",
			Category:         CategoryOrchestration,
			Description:      "ask the user clarifying questions before planning (max 5, plan-approval mode only)",
			Schema:           `{"questions": ["Which framework?", "What database?"]}`,
			ParametersSchema: `{"type":"object","properties":{"questions":{"type":"array","items":{"type":"string"},"description":"1-5 clarifying questions for the user","minItems":1,"maxItems":5}},"required":["questions"]}`,
		},
		ToolSpec{
			Name:             "synthesize",
			Category:         CategoryOrchestration,
			Description:      "present the final answer to the user",
			Schema:           `{"conclusion": "Here is the answer..."}`,
			ParametersSchema: `{"type":"object","properties":{"conclusion":{"type":"string","description":"Final answer or summary"}},"required":["conclusion"]}`,
		},
	)
}
