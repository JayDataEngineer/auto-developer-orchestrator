// Host UI dialog — currently a no-op placeholder.
// The Pi host-ui requests (confirm/select/input/editor) were Pi-specific.
// The harness doesn't expose this feature yet. This component is kept
// as a mount point for future interactive prompts (e.g. LangGraph interrupts).

import { type FC } from "react";
import {
  Dialog,
  DialogContent,
} from "@/components/ui/dialog";

export const HostUiDialog: FC = () => {
  // No active requests — render a permanently-closed dialog.
  return (
    <Dialog open={false} onOpenChange={() => {}}>
      <DialogContent />
    </Dialog>
  );
};
