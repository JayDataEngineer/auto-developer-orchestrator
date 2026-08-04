// Pux runtime provider — bridges the Agent Protocol harness to CopilotKit.
//
// Relative runtimeUrl — the Vite dev proxy (or the BFF in production) owns
// the /api/* → BFF → CopilotRuntime → Aegra chain. Works from any host
// (localhost, Tailscale IP, production domain) without env-var configuration.

import { type ReactNode } from "react";
import { CopilotKit } from "@copilotkit/react-core";

export function PuxRuntimeProvider({
  children,
}: {
  children: ReactNode;
}) {
  return (
    <CopilotKit
      runtimeUrl="/api/copilotkit"
      agent="general"
    >
      {children}
    </CopilotKit>
  );
}
