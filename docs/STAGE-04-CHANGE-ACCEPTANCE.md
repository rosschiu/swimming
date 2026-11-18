# Lesson-change acceptance

Use one guardian-linked student, two eligible published lessons and a paid package with one reserved credit. Verify each view with its real account against the same database.

| Action | Evidence |
| --- | --- |
| Guardian requests the target before the configured cutoff | The family sees the request as pending. The original coach roster still contains the swimmer. The target roster and package balance stay unchanged. |
| Assigned original coach approves | The family sees approval and the new lesson. Only the target roster contains the swimmer. Available, reserved and consumed counts match their pre-request values. |
| Coach retries the decision | The same decision is returned and no extra reservation or credit movement occurs. |
| Another swimmer takes the last target place before approval | Approval fails, the original booking and credit survive, and the guardian can see the actual request state. |
| Assigned coach rejects | The family sees the rejection. The original lesson and credit remain reserved. |
| Administrator opens request history | The original lesson, requested lesson, student, decision and dates agree with the family and coach views. |
| Unrelated guardian or coach requests a record directly | The API enforces the same scope as the screens. |

Exercise the configured cutoff immediately before, at and after the boundary in PostgreSQL integration tests. Exercise submission, decision and readback with Playwright. Use a synthetic student and simulated purchase throughout.
