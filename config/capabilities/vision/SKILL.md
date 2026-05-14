# Vision Capability

You have access to image analysis tools via the MCP media server.

## Available Functions

analyze_image(image_url, prompt): Analyze an image with a text prompt
  Pass the image URL and describe what you want to know.
  Returns a detailed text description.

## Workflow
1. Receive image URL or file path
2. analyze_image() with a specific prompt about what to extract
3. Report findings clearly

## Tips
- Be specific in your prompt: "Describe the layout and UI elements" vs "What is this?"
- For screenshots, ask about specific regions or elements
- Note text visible in images — OCR is built in
- You do NOT have a browser — you cannot navigate to URLs yourself
