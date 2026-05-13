import { createExtension } from "../../ts-tui-pi/packages/extension-sdk/index.ts";

const ext = createExtension("test_hello");

ext.tool("greet", {
	description: "Greet someone by name",
	parameters: {
		type: "object",
		properties: {
			name: { type: "string", description: "Name to greet" },
		},
		required: ["name"],
	},
}, async (params) => {
	return { content: [{ type: "text", text: `Hello, ${params.name}!` }] };
});

ext.tool("echo", {
	description: "Echo back the input text",
	parameters: {
		type: "object",
		properties: {
			text: { type: "string", description: "Text to echo" },
		},
		required: ["text"],
	},
}, async (params) => {
	return { content: [{ type: "text", text: params.text }] };
});

ext.start();
