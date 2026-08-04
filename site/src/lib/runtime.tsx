// Pux runtime provider — bridges the Agent Protocol harness to CopilotKit.
//
// Uses LangGraphHttpAgent to connect to the harness's AG-UI endpoint.
// CopilotKit handles all the streaming, state management, and UI primitives.

import { type ReactNode } from "react";
import {
  CopilotKit,
} from "@copilotkit/react-core";

const SITE_URL = import.meta.env.VITE_PUX_SITE_URL ?? "http://127.0.0.1:3001";

export function PuxRuntimeProvider({
  children,
}: {
  children: ReactNode;
}) {
  return (
    <CopilotKit
      runtimeUrl={`${SITE_URL}/api/copilotkit`}
      agent="general"
    >
      {children}
    </CopilotKit>
  );
}
