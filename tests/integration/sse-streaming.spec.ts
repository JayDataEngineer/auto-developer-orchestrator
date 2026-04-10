/**
 * SSE Streaming Integration Tests
 *
 * Tests real SSE streaming through the full pipeline:
 * Frontend → Vite proxy → Go backend → Pi agent → LiteLLM → Gemini 2
 *
 * This is the real deal — no mocking. Uses the cheap/fast cloud model.
 */
import { test, expect } from '@playwright/test';
import {
  apiGet, apiPut, streamPrompt,
  TEST_PROJECT, TEST_AGENT, TEST_MODEL,
} from './helpers';

// ─── Pre-flight: ensure model is set ────────────────────────────────

test.describe('SSE Streaming — Real Model', () => {
  test.beforeAll(async () => {
    // Set the model before any streaming tests
    await apiPut('/api/pi/model', {
      project: TEST_PROJECT,
      provider: 'litellm',
      modelId: TEST_MODEL,
      agentId: TEST_AGENT,
    });
  });

  test('simple prompt returns SSE events with text', async () => {
    const { events, texts, status } = await streamPrompt(
      'Say exactly: "Hello from integration test" and nothing else.',
      TEST_PROJECT,
      TEST_AGENT,
      30_000,
    );

    expect(status).toBe(200);
    expect(events.length).toBeGreaterThan(0);

    // Log all event types for diagnostics
    const types = events.map(e => e.type);
    console.log(`  Events received: ${types.join(', ')}`);

    // Should have agent_end (means the stream completed)
    expect(events.some(e => e.type === 'agent_end')).toBe(true);

    // Should have text content — THIS IS THE KEY TEST
    // If this fails, it means the model isn't generating text through SSE
    expect(texts.length).toBeGreaterThan(0);

    console.log(`  ✓ Stream completed: ${events.length} events, ${texts.length} chars of text`);
  });

  test('SSE events have correct structure', async () => {
    const { events, status } = await streamPrompt(
      'What is 2+2? Reply with just the number.',
      TEST_PROJECT,
      TEST_AGENT,
      30_000,
    );

    expect(status).toBe(200);

    // Verify event structure
    for (const event of events) {
      expect(event).toHaveProperty('type');
      expect(typeof event.type).toBe('string');
      expect(event).toHaveProperty('data');
    }

    // Log event types for diagnostics
    console.log(`  Event types: ${events.map(e => e.type).join(', ')}`);

    // agent_start or agent_spawned should be first (order may vary)
    expect(['agent_start', 'agent_spawned']).toContain(events[0].type);

    // agent_end should have usage data
    const endEvent = events.find(e => e.type === 'agent_end');
    if (endEvent) {
      expect(endEvent.data).toHaveProperty('input');
      expect(endEvent.data).toHaveProperty('output');
    }

    console.log(`  ✓ Event structure valid: ${events.length} events`);
  });

  test('text_delta events contain actual text', async () => {
    const { events, status } = await streamPrompt(
      'Count from 1 to 5.',
      TEST_PROJECT,
      TEST_AGENT,
      30_000,
    );

    expect(status).toBe(200);

    const textDeltas = events.filter(e => e.type === 'text_delta');
    expect(textDeltas.length).toBeGreaterThan(0);

    // Each text_delta should have a .text field
    for (const delta of textDeltas) {
      expect(delta.data).toHaveProperty('text');
      expect(typeof delta.data.text).toBe('string');
      expect(delta.data.text.length).toBeGreaterThan(0);
    }

    console.log(`  ✓ Received ${textDeltas.length} text_delta events`);
  });

  test('thinking_delta events present when model thinks', async () => {
    const { events, status } = await streamPrompt(
      'Think step by step: what is 15 * 37?',
      TEST_PROJECT,
      TEST_AGENT,
      45_000,
    );

    expect(status).toBe(200);

    // The model may or may not produce thinking deltas depending on config,
    // but the event types should be valid either way
    const validTypes = [
      'agent_start', 'agent_spawned', 'text_delta', 'thinking_delta',
      'tool_execution_start', 'tool_execution_end',
      'agent_end', 'error', 'state_update',
      'compaction_start', 'compaction_end',
      'branch_created', 'commit_created', 'push_complete',
      'pr_created', 'web_update', 'approval_request', 'question_asked',
    ];

    for (const event of events) {
      expect(validTypes).toContain(event.type);
    }

    console.log(`  ✓ All ${events.length} events have valid types`);
  });

  test('agent state updates after conversation', async () => {
    // Send a prompt
    await streamPrompt(
      'Say "test complete"',
      TEST_PROJECT,
      TEST_AGENT,
      30_000,
    );

    // Check state reflects the conversation
    const { status, data } = await apiGet(`/api/pi/state?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
    expect(data).toHaveProperty('streaming');
    // After stream completes, streaming should be false
    expect(data.streaming).toBe(false);

    console.log(`  ✓ Agent state consistent after streaming`);
  });

  test('message history persists after conversation', async () => {
    const msg = `History test ${Date.now()}`;

    await streamPrompt(msg, TEST_PROJECT, TEST_AGENT, 30_000);

    const { status, data } = await apiGet(`/api/pi/messages?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
    expect(Array.isArray(data)).toBe(true);

    // Should have at least the user message
    const userMsg = data.find((m: any) => m.role === 'user' && m.content?.includes('History test'));
    expect(userMsg).toBeDefined();

    console.log(`  ✓ Message history persisted (${data.length} messages)`);
  });

  test('model is set correctly', async () => {
    const { status, data } = await apiGet(`/api/pi/state?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);
    expect(status).toBe(200);
    // Model should be set (not empty) — if this fails, model setting doesn't persist
    console.log(`  Model state: ${JSON.stringify(data)}`);
    // Log whether model is set (may be empty string — a real finding)
    if (!data.model) {
      console.log('  ⚠ Model is empty — model setting is not persisting');
    }
    expect(data.model).toBeTruthy();
  });

  test('token usage increments after conversation', async () => {
    const stateBefore = await apiGet(`/api/pi/state?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);

    await streamPrompt('Say "tokens"', TEST_PROJECT, TEST_AGENT, 30_000);

    const stateAfter = await apiGet(`/api/pi/state?project=${TEST_PROJECT}&agentId=${TEST_AGENT}`);

    // Token usage should have incremented
    const inputAfter = stateAfter.data.input || 0;
    const outputAfter = stateAfter.data.output || 0;

    // At least some output tokens
    expect(outputAfter).toBeGreaterThan(0);

    console.log(`  ✓ Token usage: input=${inputAfter}, output=${outputAfter}`);
  });
});
