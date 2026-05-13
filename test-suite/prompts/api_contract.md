Audit the API contract of this project.

Discovery:
1. Find all API route definitions (handler files, router setup, OpenAPI specs)
2. Identify every endpoint: method, path, request schema, response schema
3. Health check the API service

Delegate to api_auditor:
- Validate schema of every response (field types, required fields, defaults)
- Test error handling (invalid input, wrong method, missing fields)
- Test streaming endpoints (SSE, WebSocket) — event format, ordering, cleanup
- Boundary test every input field
- Check authentication behavior (if applicable)

Report every endpoint's status and any contract violations.
