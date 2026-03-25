import { test, expect } from '@playwright/test';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test.describe('Build Verification Tests', () => {
  test('should have dist folder with build output', () => {
    const distPath = path.join(__dirname, '../../dist');
    expect(fs.existsSync(distPath)).toBe(true);
  });

  test('should have index.html in dist', () => {
    const indexPath = path.join(__dirname, '../../dist/index.html');
    expect(fs.existsSync(indexPath)).toBe(true);
    
    const content = fs.readFileSync(indexPath, 'utf-8');
    expect(content).toContain('<div id="root"></div>');
    expect(content).toContain('script type="module"');
  });

  test('should have JS bundle in dist/assets', () => {
    const assetsPath = path.join(__dirname, '../../dist/assets');
    expect(fs.existsSync(assetsPath)).toBe(true);
    
    const files = fs.readdirSync(assetsPath);
    const jsFiles = files.filter(f => f.endsWith('.js'));
    expect(jsFiles.length).toBeGreaterThan(0);
  });

  test('should have CSS bundle in dist/assets', () => {
    const assetsPath = path.join(__dirname, '../../dist/assets');
    const files = fs.readdirSync(assetsPath);
    const cssFiles = files.filter(f => f.endsWith('.css'));
    expect(cssFiles.length).toBeGreaterThan(0);
  });

  test('should have React components compiled', () => {
    const assetsPath = path.join(__dirname, '../../dist/assets');
    const files = fs.readdirSync(assetsPath);
    const jsFiles = files.filter(f => f.endsWith('.js'));
    
    // Check at least one JS file contains React code
    let hasReactCode = false;
    for (const file of jsFiles) {
      const content = fs.readFileSync(path.join(assetsPath, file), 'utf-8');
      if (content.includes('react') || content.includes('createElement')) {
        hasReactCode = true;
        break;
      }
    }
    expect(hasReactCode).toBe(true);
  });

  test('should have source files', () => {
    const srcPath = path.join(__dirname, '../../src');
    expect(fs.existsSync(srcPath)).toBe(true);
    
    const files = fs.readdirSync(srcPath);
    const tsxFiles = files.filter(f => f.endsWith('.tsx') || f.endsWith('.ts'));
    expect(tsxFiles.length).toBeGreaterThan(0);
  });
});
