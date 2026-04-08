import { describe, it, expect } from 'vitest';

// Test the pure helper functions extracted from PiAgentView

function formatToolArgs(name: string, args: Record<string, unknown>): string {
  if (!args) return '';
  if (name === 'read' || name === 'write' || name === 'edit') {
    return String(args.filePath || args.path || '');
  }
  if (name === 'bash') {
    return String(args.command || '').slice(0, 80);
  }
  if (name === 'grep') {
    return `${args.pattern} in ${args.path || '.'}`;
  }
  return JSON.stringify(args).slice(0, 80);
}

function formatResult(result: unknown): string {
  if (result === undefined || result === null) return '';
  if (typeof result === 'string') return result;
  return JSON.stringify(result, null, 2);
}

describe('formatToolArgs', () => {
  it('returns empty string for null args', () => {
    expect(formatToolArgs('bash', null as any)).toBe('');
  });

  it('returns empty string for undefined args', () => {
    expect(formatToolArgs('bash', undefined as any)).toBe('');
  });

  it('formats read tool args', () => {
    expect(formatToolArgs('read', { filePath: '/src/app.ts' })).toBe('/src/app.ts');
  });

  it('formats bash tool args', () => {
    expect(formatToolArgs('bash', { command: 'npm test' })).toBe('npm test');
  });

  it('truncates long bash commands', () => {
    const longCmd = 'a'.repeat(100);
    expect(formatToolArgs('bash', { command: longCmd })).toHaveLength(80);
  });

  it('formats grep tool args', () => {
    expect(formatToolArgs('grep', { pattern: 'TODO', path: 'src/' })).toBe('TODO in src/');
  });

  it('formats unknown tools as JSON', () => {
    expect(formatToolArgs('custom', { foo: 'bar' })).toBe('{"foo":"bar"}');
  });
});

describe('formatResult', () => {
  it('returns empty string for undefined', () => {
    expect(formatResult(undefined)).toBe('');
  });

  it('returns empty string for null', () => {
    expect(formatResult(null)).toBe('');
  });

  it('returns string as-is', () => {
    expect(formatResult('file contents here')).toBe('file contents here');
  });

  it('serializes objects', () => {
    const result = formatResult({ exitCode: 0, output: 'ok' });
    expect(result).toContain('exitCode');
    expect(result).toContain('ok');
  });

  it('serializes numbers', () => {
    expect(formatResult(42)).toBe('42');
  });
});
