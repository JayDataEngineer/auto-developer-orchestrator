// Wire createPiHttpClient to the backend route layer at /api/pi/**.
// Vite proxy forwards to the Node backend during dev; production serves both.

import { useMemo } from "react";
import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { createPiHttpClient, usePiRuntime } from "@assistant-ui/react-pi";

export function PiRuntimeProvider({
  threadId,
  onThreadIdChange,
  children,
}: {
  threadId?: string;
  onThreadIdChange?: (id: string | undefined) => void;
  children: React.ReactNode;
}) {
  const client = useMemo(() => createPiHttpClient({ baseUrl: "/api/pi" }), []);
  const runtime = usePiRuntime({ client, threadId, onThreadIdChange });
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      {children}
    </AssistantRuntimeProvider>
  );
}
