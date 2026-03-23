## 2026-03-22 - Form Accessibility in Modals
**Learning:** Found that inputs in `CloneModal` and `AddProjectModal` were missing proper `id` and `htmlFor` label associations. Without this, users couldn't click the label to focus the input, and screen readers lacked context.
**Action:** Always link form inputs to their labels using `htmlFor` and `id` to improve keyboard accessibility and assistive tech support.
