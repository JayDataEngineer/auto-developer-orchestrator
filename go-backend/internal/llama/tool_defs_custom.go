package llama

func init() {
	allTools = append(allTools,
		ToolSpec{
			Name:             "create_tool",
			Category:         CategoryMeta,
			Description:      "create a reusable custom tool that persists across sessions",
			Schema:           `{"name": "turnstile_solver", "description": "Solve Cloudflare Turnstile on a given URL", "script": "from seleniumbase import SB\nwith SB(uc=True) as sb:\n    sb.open(url)\n    sb.solve_captcha()\n    print(sb.get_text('body'))", "language": "python", "args_schema": "url:string"}`,
			Returns:          "Saves script to /sandbox/persist/tools/{name}.py and registers it in manifest.json. The tool becomes available via run_tool immediately.",
			ParametersSchema: `{"type":"object","properties":{"name":{"type":"string","description":"Unique tool name (lowercase, underscores only)"},"description":{"type":"string","description":"What this tool does (shown to the agent)"}},"script":{"type":"string","description":"The script content (Python, bash, or other). Use {arg_name} for parameter placeholders."},"language":{"type":"string","description":"Script language: python (default), bash, node","enum":["python","bash","node"]},"args_schema":{"type":"string","description":"Comma-separated argument definitions: name:type (e.g., 'url:string, count:int')"}},"required":["name","description","script"]}`,
		},
		ToolSpec{
			Name:             "list_tools",
			Category:         CategoryMeta,
			Description:      "list all custom tools previously created and persisted",
			Schema:           `{}`,
			Returns:          "Returns the tool manifest with name, description, args, and last_used timestamp for each tool.",
			ParametersSchema: `{"type":"object","properties":{}}`,
		},
		ToolSpec{
			Name:             "run_tool",
			Category:         CategoryMeta,
			Description:      "run a previously created custom tool by name with arguments",
			Schema:           `{"name": "turnstile_solver", "args": {"url": "https://seleniumbase.io/apps/turnstile"}}`,
			Returns:          "Executes the custom tool script with the given args. Returns stdout from the script.",
			ParametersSchema: `{"type":"object","properties":{"name":{"type":"string","description":"Name of the custom tool to run"},"args":{"type":"object","description":"Arguments to pass to the tool script","additionalProperties":true}},"required":["name"]}`,
		},
	)
}
