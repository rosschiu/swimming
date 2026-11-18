# Booking confirmation in the family notification history

A confirmed reservation should appear in the notification history for the student's linked active guardians and student login. The family can open the student profile and inspect booked lessons. Use a single notification per booking and recipient. A rejected booking creates none. Retrying after a lost response must preserve the original booking, credit movement and notifications.

Use the existing permission-checked history, read/unread state and profile link. Check current access when reading or marking a notification. Include no price or override reason. Booking confirmation belongs to the in-app channel for this slice. The existing opt-in for teaching emails covers teaching updates only. Scheduled reminders and a booking-specific email preference remain separate work.

Acceptance covers the booking guardian, another linked guardian, student, unrelated family, revoked access, transaction rollback and replay. Verify the guardian and student views against the real backend in Chromium and rerun existing teaching notification and delivery tests.
