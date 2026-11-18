# Recording attendance

This stage depends on persistent bookings, reserved credits, shared rosters and the school attendance-opening setting. An assigned coach or administrator can mark a booked student present from the class roster. The guardian and student can see that result on the booking.

## Acceptance criteria

- Before the configured opening time, the API rejects the mark and leaves attendance and credits unchanged. At the opening time it accepts the mark.
- The booking, attendance, credit consumption and audit evidence stay consistent. Replays and concurrent submissions consume the reserved credit exactly once.
- Students, guardians and other coaches cannot mark attendance. A booking from a different class cannot be marked through this roster.
- A real browser journey records attendance and verifies the family sees the result and updated credit balance.
- Review covers the API contract, database migration, existing bookings and accessibility of the roster action and status.

Absences, corrections and cancellation outcomes require additional implementation before the full lesson lifecycle is complete.
