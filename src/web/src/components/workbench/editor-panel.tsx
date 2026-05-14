import Editor from "@monaco-editor/react";

export function EditorPanel() {
	return (
		<Editor
			theme="vs-dark"
			language="markdown"
			options={{
				minimap: { enabled: false },
				fontSize: 13,
				lineNumbers: "on",
				scrollBeyondLastLine: false,
				wordWrap: "on",
				padding: { top: 8 },
			}}
		/>
	);
}
