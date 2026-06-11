import { test, expect } from '@playwright/test';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test.describe('Build Verification Tests', () => {
  test('should have source files', () => {
    const srcPath = path.join(__dirname, '../../frontend/web/src');
    expect(fs.existsSync(srcPath)).toBe(true);

    const files = fs.readdirSync(srcPath);
    const tsxFiles = files.filter(f => f.endsWith('.tsx') || f.endsWith('.ts'));
    expect(tsxFiles.length).toBeGreaterThan(0);
  });

  test('should have App component', () => {
    const appPath = path.join(__dirname, '../../frontend/web/src/app.tsx');
    expect(fs.existsSync(appPath)).toBe(true);

    const content = fs.readFileSync(appPath, 'utf-8');
    expect(content).toContain('export function App');
  });

  test('should have main entry point', () => {
    const mainPath = path.join(__dirname, '../../frontend/web/src/main.tsx');
    expect(fs.existsSync(mainPath)).toBe(true);

    const content = fs.readFileSync(mainPath, 'utf-8');
    expect(content).toContain('createRoot');
  });

  test('should have thread component', () => {
    const threadPath = path.join(__dirname, '../../frontend/web/src/components/assistant-ui/thread.tsx');
    expect(fs.existsSync(threadPath)).toBe(true);
  });

  test('should have vite config', () => {
    const vitePath = path.join(__dirname, '../../frontend/web/vite.config.ts');
    expect(fs.existsSync(vitePath)).toBe(true);
  });

  test('should have playwright config', () => {
    const pwPath = path.join(__dirname, '../playwright.config.ts');
    expect(fs.existsSync(pwPath)).toBe(true);
  });
});
