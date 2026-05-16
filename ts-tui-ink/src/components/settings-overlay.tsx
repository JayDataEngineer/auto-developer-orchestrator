import React, { useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { colors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

const THEMES = [
  { id: "default", name: "Default", desc: "Magenta brand + standard chalk colors" },
  { id: "dark", name: "Dark", desc: "Green brand, minimal contrast" },
  { id: "light", name: "Light", desc: "Blue brand, light-friendly" },
] as const;

const SECTION_KEYS = ["model", "providers", "theme", "system"] as const;
type SectionId = (typeof SECTION_KEYS)[number];

const SECTION_LABELS: Record<SectionId, string> = {
  model: "Active Model",
  providers: "Providers",
  theme: "Theme",
  system: "System",
};

export function SettingsOverlay() {
  const show = usePuxStore((s) => s.showSettingsOverlay);
  const activeModel = usePuxStore((s) => s.activeModel);
  const activeProject = usePuxStore((s) => s.activeProject);
  const activeAgentId = usePuxStore((s) => s.activeAgentId);
  const theme = usePuxStore((s) => s.theme);
  const providers = usePuxStore((s) => s.providers);
  const toggleProvidersOverlay = usePuxStore((s) => s.toggleProvidersOverlay);
  const toggleModelPicker = usePuxStore((s) => s.toggleModelPicker);
  const closeSettingsOverlay = usePuxStore((s) => s.closeSettingsOverlay);
  const setTheme = usePuxStore((s) => s.setTheme);
  const [focusIdx, setFocusIdx] = React.useState(0);

  useInput(
    useCallback(
      (input: string, key: any) => {
        if (!show) return;

        if (key.escape) {
          closeSettingsOverlay();
          return;
        }

        if (key.upArrow) {
          setFocusIdx((prev) => Math.max(0, prev - 1));
          return;
        }
        if (key.downArrow) {
          setFocusIdx((prev) => Math.min(SECTION_KEYS.length - 1, prev + 1));
          return;
        }

        if (key.return) {
          const section = SECTION_KEYS[focusIdx];
          if (section === "providers") {
            closeSettingsOverlay();
            toggleProvidersOverlay();
          } else if (section === "model") {
            closeSettingsOverlay();
            toggleModelPicker();
          }
          return;
        }
      },
      [show, focusIdx, closeSettingsOverlay, toggleProvidersOverlay, toggleModelPicker],
    ),
  );

  if (!show) return null;

  // Determine active provider
  const activeProvider = Object.entries(providers).find(([, info]) =>
    info.models?.some((m) => m.id === activeModel),
  )?.[0] || "";

  const providerCount = Object.keys(providers).length;

  return (
    <Box flexDirection="column" flexGrow={1}>
      {/* Header */}
      <Box backgroundColor="cyan" paddingX={1}>
        <Text bold> {symbols.dot} Settings</Text>
      </Box>
      <Text dimColor>{'═'.repeat(80)}</Text>

      {/* Content */}
      <Box flexGrow={1} flexDirection="column" paddingX={1} paddingY={1}>
        {SECTION_KEYS.map((section, i) => {
          const focused = i === focusIdx;
          const prefix = focused ? ">" : " ";
          return (
            <Box key={section} flexDirection="column" marginBottom={1}>
              {/* Section header */}
              <Box>
                <Text color={focused ? colors.brand : undefined} bold>
                  {prefix} {SECTION_LABELS[section]}
                </Text>
              </Box>

              {/* Section content */}
              <Box paddingLeft={3} flexDirection="column">
                {section === "model" && (
                  <>
                    <Text dimColor>
                      {BLOCKQUOTE_BAR} Model: {activeModel || "not set"}
                    </Text>
                    <Text dimColor>
                      {BLOCKQUOTE_BAR} Provider: {activeProvider || "not set"}
                    </Text>
                    {focused && (
                      <Text color="gray">  Enter to open model picker</Text>
                    )}
                  </>
                )}

                {section === "providers" && (
                  <>
                    <Text dimColor>
                      {BLOCKQUOTE_BAR} {providerCount} provider{providerCount !== 1 ? "s" : ""} configured
                    </Text>
                    {Object.entries(providers).slice(0, 5).map(([name, info]) => (
                      <Text key={name} dimColor>
                        {BLOCKQUOTE_BAR}  {name} ({info.status})
                      </Text>
                    ))}
                    {Object.keys(providers).length > 5 && (
                      <Text dimColor>{BLOCKQUOTE_BAR}  ... and {Object.keys(providers).length - 5} more</Text>
                    )}
                    {focused && (
                      <Text color="gray">  Enter to open providers panel</Text>
                    )}
                  </>
                )}

                {section === "theme" && (
                  <>
                    {THEMES.map((t) => {
                      const active = t.id === theme;
                      return (
                        <Box key={t.id}>
                          <Text dimColor>
                            {BLOCKQUOTE_BAR} {active ? symbols.check : " "} {t.name}
                          </Text>
                          {active && <Text color="gray">  (current)</Text>}
                        </Box>
                      );
                    })}
                    <Box marginTop={1}>
                      {THEMES.map((t) => (
                        <Text key={t.id} dimColor>
                          {t.id === "default" ? "" : " "}[{t.id[0].toUpperCase()}] {t.name}
                        </Text>
                      ))}
                    </Box>
                  </>
                )}

                {section === "system" && (
                  <>
                    <Text dimColor>
                      {BLOCKQUOTE_BAR} Project: {activeProject || "none"}
                    </Text>
                    <Text dimColor>
                      {BLOCKQUOTE_BAR} Agent: {activeAgentId || "none"}
                    </Text>
                  </>
                )}
              </Box>
            </Box>
          );
        })}
      </Box>

      {/* Footer */}
      <Text dimColor>{'═'.repeat(80)}</Text>
      <Box paddingX={1}>
        <Text dimColor>
          ↑↓ navigate · Enter select · Esc close
        </Text>
      </Box>
    </Box>
  );
}
