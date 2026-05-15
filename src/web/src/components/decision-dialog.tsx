import { usePuxStore } from "@/lib/pux-store";
import { DecisionQuestion } from "./decision-question";
import { DecisionApproval } from "./decision-approval";
import { DecisionPlanReview } from "./decision-plan-review";

/**
 * DecisionDialog — routes to the correct sub-component by hint.
 * Wired into app.tsx as an overlay when pendingDecision is set.
 */
export function DecisionDialog() {
	const pending = usePuxStore((s) => s.pendingDecision);
	if (!pending) return null;

	switch (pending.hint) {
		case "question":
			return <DecisionQuestion />;
		case "approval":
			return <DecisionApproval />;
		case "plan_review":
			return <DecisionPlanReview />;
		default:
			return <DecisionQuestion />;
	}
}
