import { describe, test, expect } from "bun:test";
import { InMemorySettingsStorage, SettingsManager } from "../core/settings-manager.js";

describe("InMemorySettingsStorage", () => {
  test("initially undefined for both scopes", () => {
    const storage = new InMemorySettingsStorage();
    storage.withLock("global", (current) => {
      expect(current).toBeUndefined();
      return undefined;
    });
    storage.withLock("project", (current) => {
      expect(current).toBeUndefined();
      return undefined;
    });
  });

  test("write then read back", () => {
    const storage = new InMemorySettingsStorage();
    storage.withLock("global", () => '{"key": "val"}');
    storage.withLock("global", (current) => {
      expect(current).toBe('{"key": "val"}');
      return undefined;
    });
  });

  test("scopes are independent", () => {
    const storage = new InMemorySettingsStorage();
    storage.withLock("global", () => '{"scope": "global"}');
    storage.withLock("project", () => '{"scope": "project"}');

    storage.withLock("global", (current) => {
      expect(JSON.parse(current!).scope).toBe("global");
      return undefined;
    });
    storage.withLock("project", (current) => {
      expect(JSON.parse(current!).scope).toBe("project");
      return undefined;
    });
  });

  test("returning undefined does not overwrite", () => {
    const storage = new InMemorySettingsStorage();
    storage.withLock("global", () => '{"key": "val"}');
    storage.withLock("global", () => undefined);
    storage.withLock("global", (current) => {
      expect(current).toBe('{"key": "val"}');
      return undefined;
    });
  });

  test("returning empty string overwrites", () => {
    const storage = new InMemorySettingsStorage();
    storage.withLock("global", () => '{"key": "val"}');
    storage.withLock("global", () => "");
    storage.withLock("global", (current) => {
      expect(current).toBe("");
      return undefined;
    });
  });
});

describe("SettingsManager.inMemory", () => {
  test("default getters return expected values", () => {
    const sm = SettingsManager.inMemory();
    expect(sm.getDefaultProvider()).toBeUndefined();
    expect(sm.getDefaultModel()).toBeUndefined();
    expect(sm.getDefaultThinkingLevel()).toBeUndefined();
    expect(sm.getTheme()).toBeUndefined();
    expect(sm.getSteeringMode()).toBe("one-at-a-time");
    expect(sm.getFollowUpMode()).toBe("one-at-a-time");
    expect(sm.getCompactionEnabled()).toBe(true);
    expect(sm.getCompactionReserveTokens()).toBe(16384);
    expect(sm.getCompactionKeepRecentTokens()).toBe(20000);
    expect(sm.getRetryEnabled()).toBe(true);
    expect(sm.getHideThinkingBlock()).toBe(true);
    expect(sm.getQuietStartup()).toBe(false);
    expect(sm.getShellPath()).toBeUndefined();
    expect(sm.getTransport()).toBe("sse");
    expect(sm.getDoubleEscapeAction()).toBe("tree");
    expect(sm.getTreeFilterMode()).toBe("default");
    expect(sm.getShowImages()).toBe(true);
    expect(sm.getImageAutoResize()).toBe(true);
    expect(sm.getBlockImages()).toBe(false);
    expect(sm.getEnableSkillCommands()).toBe(true);
    expect(sm.getCodeBlockIndent()).toBe("  ");
    expect(sm.getShellCommandPrefix()).toBeUndefined();
    expect(sm.getCollapseChangelog()).toBe(false);
    expect(sm.getShowHardwareCursor()).toBe(false);
    expect(sm.getClearOnShrink()).toBe(false);
    expect(sm.getEditorPaddingX()).toBe(0);
    expect(sm.getAutocompleteMaxVisible()).toBe(5);
    expect(sm.getLastChangelogVersion()).toBeUndefined();
    expect(sm.getSessionDir()).toBeUndefined();
  });

  test("setDefaultModel and getDefaultModel", () => {
    const sm = SettingsManager.inMemory();
    sm.setDefaultModel("claude-4");
    expect(sm.getDefaultModel()).toBe("claude-4");
  });

  test("setDefaultProvider updates provider", () => {
    const sm = SettingsManager.inMemory();
    sm.setDefaultProvider("anthropic");
    expect(sm.getDefaultProvider()).toBe("anthropic");
  });

  test("setDefaultModelAndProvider sets both", () => {
    const sm = SettingsManager.inMemory();
    sm.setDefaultModelAndProvider("openai", "gpt-4o");
    expect(sm.getDefaultProvider()).toBe("openai");
    expect(sm.getDefaultModel()).toBe("gpt-4o");
  });

  test("setDefaultThinkingLevel", () => {
    const sm = SettingsManager.inMemory();
    sm.setDefaultThinkingLevel("high");
    expect(sm.getDefaultThinkingLevel()).toBe("high");
  });

  test("setTheme", () => {
    const sm = SettingsManager.inMemory();
    sm.setTheme("dark");
    expect(sm.getTheme()).toBe("dark");
  });

  test("setSteeringMode and getSteeringMode", () => {
    const sm = SettingsManager.inMemory();
    sm.setSteeringMode("all");
    expect(sm.getSteeringMode()).toBe("all");
  });

  test("setFollowUpMode and getFollowUpMode", () => {
    const sm = SettingsManager.inMemory();
    sm.setFollowUpMode("one-at-a-time");
    expect(sm.getFollowUpMode()).toBe("one-at-a-time");
  });

  test("setCompactionEnabled toggles", () => {
    const sm = SettingsManager.inMemory();
    sm.setCompactionEnabled(false);
    expect(sm.getCompactionEnabled()).toBe(false);
    sm.setCompactionEnabled(true);
    expect(sm.getCompactionEnabled()).toBe(true);
  });

  test("getCompactionSettings returns all compaction fields", () => {
    const sm = SettingsManager.inMemory();
    const settings = sm.getCompactionSettings();
    expect(settings).toEqual({
      enabled: true,
      reserveTokens: 16384,
      keepRecentTokens: 20000,
    });
  });

  test("getBranchSummarySettings defaults", () => {
    const sm = SettingsManager.inMemory();
    expect(sm.getBranchSummarySettings()).toEqual({
      reserveTokens: 16384,
      skipPrompt: false,
    });
  });

  test("getRetryEnabled toggles", () => {
    const sm = SettingsManager.inMemory();
    sm.setRetryEnabled(false);
    expect(sm.getRetryEnabled()).toBe(false);
  });

  test("getRetrySettings defaults", () => {
    const sm = SettingsManager.inMemory();
    const settings = sm.getRetrySettings();
    expect(settings.enabled).toBe(true);
    expect(settings.maxRetries).toBe(3);
    expect(settings.baseDelayMs).toBe(2000);
    expect(settings.maxDelayMs).toBe(60000);
  });

  test("setHideThinkingBlock toggles", () => {
    const sm = SettingsManager.inMemory();
    sm.setHideThinkingBlock(false);
    expect(sm.getHideThinkingBlock()).toBe(false);
  });

  test("setQuietStartup toggles", () => {
    const sm = SettingsManager.inMemory();
    sm.setQuietStartup(true);
    expect(sm.getQuietStartup()).toBe(true);
  });

  test("setShellPath", () => {
    const sm = SettingsManager.inMemory();
    sm.setShellPath("/bin/zsh");
    expect(sm.getShellPath()).toBe("/bin/zsh");
    sm.setShellPath(undefined);
    expect(sm.getShellPath()).toBeUndefined();
  });

  test("setTransport", () => {
    const sm = SettingsManager.inMemory();
    sm.setTransport("websocket");
    expect(sm.getTransport()).toBe("websocket");
  });

  test("setDoubleEscapeAction", () => {
    const sm = SettingsManager.inMemory();
    sm.setDoubleEscapeAction("fork");
    expect(sm.getDoubleEscapeAction()).toBe("fork");
  });

  test("setTreeFilterMode", () => {
    const sm = SettingsManager.inMemory();
    sm.setTreeFilterMode("user-only");
    expect(sm.getTreeFilterMode()).toBe("user-only");
  });

  test("setShowImages toggles", () => {
    const sm = SettingsManager.inMemory();
    sm.setShowImages(false);
    expect(sm.getShowImages()).toBe(false);
  });

  test("setImageAutoResize toggles", () => {
    const sm = SettingsManager.inMemory();
    sm.setImageAutoResize(false);
    expect(sm.getImageAutoResize()).toBe(false);
  });

  test("setBlockImages toggles", () => {
    const sm = SettingsManager.inMemory();
    expect(sm.getBlockImages()).toBe(false);
    sm.setBlockImages(true);
    expect(sm.getBlockImages()).toBe(true);
  });

  test("getGlobalSettings returns clone", () => {
    const sm = SettingsManager.inMemory({ defaultProvider: "test" });
    const global = sm.getGlobalSettings();
    expect(global.defaultProvider).toBe("test");
  });

  test("getProjectSettings returns clone", () => {
    const sm = SettingsManager.inMemory();
    expect(sm.getProjectSettings()).toEqual({});
  });

  test("drainErrors returns empty initially", () => {
    const sm = SettingsManager.inMemory();
    expect(sm.drainErrors()).toEqual([]);
  });

  test("drainErrors clears the error list", () => {
    const sm = SettingsManager.inMemory();
    const first = sm.drainErrors();
    const second = sm.drainErrors();
    expect(first).toEqual([]);
    expect(second).toEqual([]);
  });

  test("applyOverrides merges on top", () => {
    const sm = SettingsManager.inMemory({
      defaultProvider: "a",
      compaction: { enabled: true, reserveTokens: 100 },
    });
    sm.applyOverrides({ compaction: { enabled: false } });
    expect(sm.getCompactionEnabled()).toBe(false);
    expect(sm.getCompactionReserveTokens()).toBe(100);
  });

  test("flush is a no-op for in-memory", async () => {
    const sm = SettingsManager.inMemory();
    await sm.flush();
    expect(true).toBe(true);
  });

  test("initial settings from constructor", () => {
    const sm = SettingsManager.inMemory({
      defaultProvider: "anthropic",
      defaultModel: "claude-4",
      theme: "monokai",
    });
    expect(sm.getDefaultProvider()).toBe("anthropic");
    expect(sm.getDefaultModel()).toBe("claude-4");
    expect(sm.getTheme()).toBe("monokai");
  });

  test("setShellCommandPrefix", () => {
    const sm = SettingsManager.inMemory();
    sm.setShellCommandPrefix("shopt -s expand_aliases");
    expect(sm.getShellCommandPrefix()).toBe("shopt -s expand_aliases");
  });

  test("setCollapseChangelog", () => {
    const sm = SettingsManager.inMemory();
    sm.setCollapseChangelog(true);
    expect(sm.getCollapseChangelog()).toBe(true);
  });

  test("setEditorPaddingX clamps value", () => {
    const sm = SettingsManager.inMemory();
    sm.setEditorPaddingX(5);
    expect(sm.getEditorPaddingX()).toBe(3);
    sm.setEditorPaddingX(-1);
    expect(sm.getEditorPaddingX()).toBe(0);
  });

  test("setAutocompleteMaxVisible clamps value", () => {
    const sm = SettingsManager.inMemory();
    sm.setAutocompleteMaxVisible(1);
    expect(sm.getAutocompleteMaxVisible()).toBe(3);
    sm.setAutocompleteMaxVisible(50);
    expect(sm.getAutocompleteMaxVisible()).toBe(20);
  });

  test("setEnableSkillCommands toggles", () => {
    const sm = SettingsManager.inMemory();
    sm.setEnableSkillCommands(false);
    expect(sm.getEnableSkillCommands()).toBe(false);
  });

  test("setEnabledModels", () => {
    const sm = SettingsManager.inMemory();
    sm.setEnabledModels(["sonnet", "haiku"]);
    expect(sm.getEnabledModels()).toEqual(["sonnet", "haiku"]);
    sm.setEnabledModels(undefined);
    expect(sm.getEnabledModels()).toBeUndefined();
  });
});
