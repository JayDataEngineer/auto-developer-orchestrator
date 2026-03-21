# E2E Testing Guide

## Overview

This project includes comprehensive E2E tests for both API endpoints and frontend components.

## Test Structure

```
tests/e2e/
├── api.test.ts          # API endpoint tests
├── components.test.ts   # Component import tests
└── README.md           # This file
```

## Running Tests

### Prerequisites

1. **Start the development server:**
   ```bash
   make dev-up
   ```

2. **Install test dependencies:**
   ```bash
   make install
   ```

### Run All E2E Tests

```bash
make test-e2e
```

Or directly:

```bash
npm run test:e2e
```

### Run Specific Test Files

```bash
# API tests only
npx vitest run tests/e2e/api.test.ts

# Component tests only
npx vitest run tests/e2e/components.test.ts
```

### Run Tests in Watch Mode

```bash
npx vitest tests/e2e/
```

## Test Coverage

### API Tests (`api.test.ts`)

| Category | Endpoints Tested |
|----------|-----------------|
| **Health & Status** | `/`, `/api/status` |
| **Projects API** | `GET /api/projects`, `POST /api/projects/add` |
| **Checklist API** | `GET /api/checklist`, `POST /api/checklist/update` |
| **AI Config API** | `GET /api/config/ai`, `POST /api/config/ai` |
| **System Config API** | `GET /api/config/system`, `POST /api/config/system` |
| **Settings API** | `POST /api/settings/mode` |
| **Task Dispatch** | `POST /api/dispatch` |
| **Merge API** | `POST /api/merge` |
| **Test Generation** | `POST /api/generate-tests`, `POST /api/run-tests` |
| **Clone API** | `POST /api/clone` |
| **Deep Agent** | `POST /api/ai/generate-todos` |
| **Error Handling** | 404, malformed JSON, missing fields |
| **Integration Flow** | Full workflow test |

### Component Tests (`components.test.ts`)

| Category | Components Tested |
|----------|------------------|
| **Core Components** | App, Header, Sidebar |
| **Feature Components** | Checklist, Terminal, CurrentTaskCard |
| **Modules** | deepAgent, utils |

## Writing New Tests

### API Test Example

```typescript
import { describe, it, expect } from 'vitest';
import fetch from 'node-fetch';

const BASE_URL = 'http://localhost:3847';

describe('My New API Test', () => {
  it('should do something', async () => {
    const response = await fetch(`${BASE_URL}/api/endpoint`);
    expect(response.status).toBe(200);
    
    const data = await response.json();
    expect(data).toHaveProperty('expectedProperty');
  });
});
```

### Component Test Example

```typescript
import { describe, it, expect } from 'vitest';

describe('My Component Test', () => {
  it('should import component', async () => {
    const { MyComponent } = await import('../src/components/MyComponent');
    expect(MyComponent).toBeDefined();
  });
});
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: E2E Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '22'
      
      - name: Install dependencies
        run: npm ci
      
      - name: Start server
        run: npm run dev &
        env:
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
      
      - name: Wait for server
        run: sleep 10
      
      - name: Run E2E tests
        run: npm run test:e2e
```

## Troubleshooting

### Server Not Responding

```bash
# Check if server is running
curl http://localhost:3847

# View server logs
make logs

# Restart server
make dev-restart
```

### Test Timeout Issues

Increase timeout in `vitest.config.e2e.ts`:

```typescript
export default defineConfig({
  test: {
    testTimeout: 60000, // Increase to 60s
    hookTimeout: 60000,
  },
});
```

### Missing Dependencies

```bash
# Reinstall dependencies
rm -rf node_modules package-lock.json
npm install
```

## Test Best Practices

1. **Isolation**: Each test should be independent
2. **Cleanup**: Clean up test data after tests
3. **Mocking**: Mock external services (AI APIs, GitHub)
4. **Assertions**: Use descriptive assertions
5. **Timeouts**: Set appropriate timeouts for async operations
6. **Error Cases**: Test both success and failure scenarios

## Coverage Reports

Generate coverage report (future enhancement):

```bash
npx vitest run --coverage
```

## Performance Benchmarks

Typical test run times:
- **API Tests**: ~5-10 seconds
- **Component Tests**: ~2-5 seconds
- **Total E2E Suite**: ~10-15 seconds

## Contributing

When adding new API endpoints or components:
1. Add corresponding E2E tests
2. Update this README
3. Ensure tests pass locally before pushing
4. Consider edge cases and error scenarios
