package llama

func init() {
	allTools = append(allTools,
		ToolSpec{
			Name:             "update_memory",
			Category:         CategoryMeta,
			Description:      "save important information to project memory for future sessions",
			Schema:           `{"content": "Key finding: database uses PostgreSQL 15...", "section": "Project Facts"}`,
			Returns:          "Saves to MEMORY.md in the project directory. Persisted across sessions. Max 200 lines. Sections: User Preferences, Project Facts, Feedback, Reference.",
			ParametersSchema: `{"type":"object","properties":{"content":{"type":"string","description":"Information to save to project memory."},"section":{"type":"string","description":"Optional section to update: User Preferences, Project Facts, Feedback, Reference. Without this, replaces entire memory."}},"required":["content"]}`,
		},
		ToolSpec{
			Name:             "wait",
			Category:         CategoryMeta,
			Description:      "wait for a specified duration before proceeding",
			Schema:           `{"seconds": 2}`,
			Returns:          "Waits the specified number of seconds. Use after navigation or actions that need time to take effect.",
			ParametersSchema: `{"type":"object","properties":{"seconds":{"type":"integer","description":"Seconds to wait (1-30)"}},"required":["seconds"]}`,
		},
		ToolSpec{
			Name:             "yield_artifact",
			Category:         CategoryMeta,
			Description:      "signal task completion and return output to orchestrator",
			Schema:           `{"output": "Task completed. Summary of results..."}`,
			Returns:          "Call this when your assigned task is done.",
			ParametersSchema: `{"type":"object","properties":{"output":{"type":"string","description":"Task output or summary"}},"required":["output"]}`,
		},
		ToolSpec{
			Name:             "ask_user",
			Category:         CategoryMeta,
			Description:      "ask the user a clarifying question and wait for their response",
			Schema:           `{"question": "Which framework would you like to use: React or Vue?"}`,
			Returns:          "Returns the user's answer as text. Use this before starting complex tasks.",
			ParametersSchema: `{"type":"object","properties":{"question":{"type":"string","description":"Question to ask the user"}},"required":["question"]}`,
		},
		ToolSpec{
			Name:             "read_skill",
			Category:         CategoryMeta,
			Description:      "load the full instructions for a skill available in the <available_skills> list",
			Schema:           `{"skill_name": "docker-expert"}`,
			Returns:          "Returns the skill's full instructions. Use this before performing tasks that match a skill's description. Skills are listed in the system prompt.",
			ParametersSchema: `{"type":"object","properties":{"skill_name":{"type":"string","description":"Name of the skill to load, from the <available_skills> list"}},"required":["skill_name"]}`,
		},
	)
}
