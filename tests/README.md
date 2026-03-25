# E2E Tests with Playwright

Automated end-to-end tests with visual screenshot capture.

## Quick Start

```bash
# Install Playwright browsers
task test:playwright:install

# Run all tests
task test:e2e

# Run visual screenshot tests
task test:screenshot

# Run tests with UI
task test:playwright:ui
```

## Test Commands

| Command | Description |
|---------|-------------|
| `task test:e2e` | Run all E2E tests |
| `task test:screenshot` | Run visual tests & capture screenshots |
| `task test:playwright:ui` | Open Playwright UI |
| `task test:playwright:headed` | Run tests in visible browser |
| `task test:playwright:debug` | Debug tests |

## Screenshot Tests

Visual tests capture screenshots of:

1. **Main page** - Full application view
2. **Sidebar** - Navigation panel
3. **Terminal** - Log output panel
4. **Header** - Top bar with controls
5. **Activity tab** - Activity view
6. **Project selector** - Project dropdown
7. **Mobile view** - Responsive (375x667)
8. **Tablet view** - Responsive (768x1024)
9. **Desktop wide** - Responsive (1920x1080)
10. **UI theme** - Glass morphism styling

Screenshots are saved to: `tests/e2e/screenshots/`

## Test Files

- `tests/e2e/visual.spec.ts` - Visual/screenshot tests
- `tests/e2e/functional.spec.ts` - Functional tests
- `playwright.config.ts` - Playwright configuration

## Configuration

The Playwright config is set to:
- **Base URL:** http://localhost:5174
- **Browsers:** Chromium, Firefox, WebKit
- **Screenshots:** On failure only
- **Video:** On failure only
- **Trace:** On first retry

## CI/CD Integration

```yaml
# Example GitHub Actions
- name: Install Playwright
  run: task test:playwright:install

- name: Run E2E tests
  run: task test:e2e
```

## Debugging

```bash
# Debug specific test
npx playwright test tests/e2e/visual.spec.ts --debug

# Run with browser visible
npx playwright test --headed

# Run with UI
npx playwright test --ui
```

## Troubleshooting

### Tests fail to connect
Make sure dev server is running:
```bash
task dev:up
```

### Browser not found
Install browsers:
```bash
task test:playwright:install
```

### Port conflicts
Update `playwright.config.ts` baseURL to match your dev server port.
