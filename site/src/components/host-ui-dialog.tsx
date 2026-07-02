// Renders pending Pi host-ui requests (confirm / select / input / editor)
// as a modal Dialog. Drives responses through usePiHostUiRequests().respond().
//
// Mount once at the app root. Stays invisible until a request arrives.

import { useEffect, useState, type FC, type FormEvent } from "react";
import { usePiHostUiRequests } from "@assistant-ui/react-pi";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export const HostUiDialog: FC = () => {
  const { requests, respond } = usePiHostUiRequests();
  const request = requests[0];
  const isOpen = !!request;

  // Local state for input/select/editor values, reset when the request changes.
  const [textValue, setTextValue] = useState("");
  useEffect(() => {
    if (!request) return;
    if (request.kind === "input") setTextValue("");
    if (request.kind === "editor") setTextValue(request.prefill ?? "");
  }, [request?.id]);

  if (!request) {
    return (
      <Dialog open={false} onOpenChange={() => {}}>
        <DialogContent />
      </Dialog>
    );
  }

  const dismiss = () =>
    respond({ requestId: request.id, dismissed: true }).catch(() => {});

  const onOpenChange = (open: boolean) => {
    if (!open) dismiss();
  };

  const confirm = (confirmed: boolean) =>
    respond({ requestId: request.id, confirmed }).catch(() => {});

  const submitValue = (e: FormEvent) => {
    e.preventDefault();
    respond({ requestId: request.id, value: textValue }).catch(() => {});
  };

  const submitSelect = (value: string) =>
    respond({ requestId: request.id, value }).catch(() => {});

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{request.title}</DialogTitle>
          {request.kind === "confirm" && (
            <DialogDescription>{request.message}</DialogDescription>
          )}
          {request.kind === "select" && (
            <DialogDescription>Pick one option.</DialogDescription>
          )}
          {request.kind === "input" && (
            <DialogDescription>Provide a response.</DialogDescription>
          )}
          {request.kind === "editor" && (
            <DialogDescription>Edit the content below.</DialogDescription>
          )}
        </DialogHeader>

        {request.kind === "confirm" && (
          <DialogFooter>
            <Button variant="outline" onClick={() => confirm(false)}>
              No
            </Button>
            <Button onClick={() => confirm(true)}>Yes</Button>
          </DialogFooter>
        )}

        {request.kind === "select" && (
          <div className="flex flex-col gap-1.5">
            {request.options.map((opt, i) => (
              <button
                key={i}
                type="button"
                onClick={() => submitSelect(opt)}
                className="rounded-md border border-border px-3 py-2 text-left text-sm hover:bg-accent"
              >
                {opt}
              </button>
            ))}
          </div>
        )}

        {request.kind === "input" && (
          <form onSubmit={submitValue} className="flex flex-col gap-3">
            <input
              autoFocus
              value={textValue}
              onChange={(e) => setTextValue(e.target.value)}
              placeholder={request.placeholder ?? ""}
              className={cn(
                "w-full rounded-md border border-border bg-background px-3 py-2 text-sm",
                "focus:outline-none focus:ring-2 focus:ring-ring",
              )}
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={dismiss}>
                Cancel
              </Button>
              <Button type="submit" disabled={!textValue}>
                Submit
              </Button>
            </DialogFooter>
          </form>
        )}

        {request.kind === "editor" && (
          <form onSubmit={submitValue} className="flex flex-col gap-3">
            <textarea
              autoFocus
              value={textValue}
              onChange={(e) => setTextValue(e.target.value)}
              rows={14}
              className={cn(
                "w-full resize-none rounded-md border border-border bg-background p-3 font-mono text-xs",
                "focus:outline-none focus:ring-2 focus:ring-ring",
              )}
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={dismiss}>
                Cancel
              </Button>
              <Button type="submit">Submit</Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
};
