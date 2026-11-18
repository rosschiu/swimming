# Attendance and corrections

The assigned class coach or an administrator records attendance from the existing roster. The booking stays on the student's lesson list. Guardians and students can see the recorded outcome; editing is restricted to authorized staff. An unmarked lesson stays unmarked until someone explicitly submits an outcome.

## Confirmed rules

Booking reserves a credit. Attendance consumes the reserved credit. Absence and illness use the applicable package policy and retain the credit by default. Attendance opens 15 minutes after lesson start by default, and the default correction window is three days. Administrators can configure these timings.

## Transaction and history

Keep the attendance outcome separate from the booking identity. Marking attendance must preserve the booked place, capacity protection and historical roster. Acquire the scheduling and identity locks in booking order, verify the current class coach or administrator, then lock the booking and the exact package charged. Recheck role and assignment before returning a replay.

Store each accepted mark or correction with its actor, expected version, time and resulting credit movement. Require a reason for a correction. Moving one reserved credit to consumed, available or expired, inserting the attendance revision and recording the audit event share a transaction. A correction changes only the difference between the prior and new outcomes. A failed correction leaves the prior mark and all balances intact. A correction with zero credit effect still needs an attendance history entry.

## Decisions awaiting confirmation

- Does the three-day deadline run from lesson end for both the initial mark and corrections? May absence and illness be finalized during class, or only at its end?
- If a correction changes a retained credit to a consumed one after the family has reused it, can another unused credit from the same package be used? If there is none, should an administrator review the correction? Holding the returned credit throughout the window would change when a family can book again.

## Acceptance evidence

Exercise current assignment and reassignment, stale versions, concurrent submissions, exact timing boundaries, overnight lessons, retention changes from package defaults, expiry before correction, recovery after expiry, rollback on audit failure and identical retries. A real-stack browser journey should mark a lesson from the coach roster and confirm the guardian sees the same outcome and balance. Independent review must verify that corrections cannot create additional credits.
