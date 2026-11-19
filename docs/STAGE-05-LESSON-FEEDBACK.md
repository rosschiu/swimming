# Lesson-linked progress feedback

Planning draft based on main at `55a4a9a`. Let staff share a progress note for a specific swimmer and lesson. Reuse the student progress stream, audited corrections, scoped teaching notifications and bilingual UI.

## Existing boundaries

- A profile note currently belongs to a student. Administrators and coaches with a current student assignment can create it. Administrators and its currently assigned author can correct it with a reason and version. A linked guardian and the student can read it; staff and families still require current access.
- Class roster access is based on assignment to the class. Being the class coach and being assigned to the swimmer’s profile are distinct permissions. A lesson change moves a booking to another class while preserving its ID. The link needs an explicit historical class identity.
- Note creation already commits the note, audit event and teaching notifications together. The current replay key compares actor, student and original text. The new link must participate in this comparison. Seeded and existing generic notes have no lesson association.

## Acceptance

| Area | Expected behavior |
|---|---|
| Entry point | Authorized staff can compose from an eligible swimmer’s roster row. Show the swimmer, date in Hong Kong and lesson name before sharing. Preserve the existing general note entry on the student profile. |
| Validate association | Use a concrete booking and class identity. Verify the booking belongs to the target swimmer and supplied class, the staff member meets current scope, and the lesson meets the confirmed eligibility rule. Reject stale or forged links and races with reassignment or booking changes. |
| Persistence and compatibility | Extend the existing note model and API with optional lesson context. Existing notes remain readable, creatable and correctable with their existing shapes. Bind new feedback to the actual lesson at creation and preserve that association on text correction or replay. Return only safe lesson context to scoped readers. |
| Family read | Linked guardian and self-service student see the same note once in student progress, with lesson label and Hong Kong date. Unrelated accounts cannot fetch the note or infer its lesson. A note link must not grant access to another pupil, class roster or booking history. |
| Replay and corrections | Concurrent identical submissions create one note, audit entry and notification per eligible recipient. A reused request ID with another booking or class conflicts. Recheck current permissions before replay. Preserve edit version checks and the author/administrator correction model, subject to the confirmed coach access rule. |
| Failure and audit | An audit or notification write failure rolls back the note. Audit records lesson association alongside the existing note evidence. Existing opt-in teaching email behavior applies, with a synthetic inbox in tests. Sharing does not alter attendance, booking, credit or milestone state. |

## Confirmed decisions (2026-11-19)

The owner confirmed the recommended defaults: the class coach (with current student assignment) or an administrator writes the note; only for a swimmer marked present; several notes per lesson; visible to the family as soon as it is shared. Notes may carry text, voice recordings, pictures and video. Media files are stored privately on the server with per-file size and type limits, served only to readers of the note, and never sent by email.

## Material decisions (as proposed)

- **Class coach versus profile assignment.** Recommend requiring both current class coach and current student assignment for coach creation. This matches the existing read and correction scope. If covering coaches must share feedback for a student outside their assignment, define whether they receive access only to that class note and whether they can later correct it. Keep full student progress scope unchanged.
- **Lesson eligibility.** Recommend a confirmed present outcome for the initial slice. This ties feedback to a taught lesson and prevents a pending move from changing its context. Confirm whether administrators or covering coaches need to record feedback after class when attendance is unmarked, or link historical absence and illness notes.
- **Multiplicity and history.** Recommend permitting multiple ordinary notes for one booking, each protected by the current replay key. Keep class/booking reference immutable and store a family-safe lesson label/date snapshot so later class edits cannot rewrite recorded context. Confirm if product intent is a single editable evaluation per lesson.

## Verification

PostgreSQL integration covers legacy notes and migration, mismatch and missing booking/class, cross-role scope and revocation races, reassignment, stale booking state, immutable association, replay uniqueness, correction conflict and notification rollback. Frontend tests cover role controls, bilingual lesson labels, stale options and switching rows/accounts. A real browser journey starts in a coach roster, marks present, shares feedback, checks guardian and student history, and confirms unrelated users have no access.

## Delivered: lesson feedback text (stage 8a of the mobile-first plan)

- A roster row marked present shows **Share feedback**. The class coach or an administrator writes the feedback there; it is saved as a progress note linked to that booking (migration 32 adds `booking_id`, `class_id` and a snapshot of the lesson name and start time to `progress_notes`).
- `POST /api/v1/students/{id}/progress-notes` accepts an optional `bookingId`. The booking must belong to the swimmer and have a present outcome (`409 lesson_not_eligible` otherwise); a coach must be the class's current coach and still assigned to the swimmer (`403`). The booking, class and attendance rows are locked while the note is written, so a concurrent correction cannot slip in.
- Several notes per lesson are allowed. A request ID reused with a different lesson, or with none, returns `409 request_conflict`.
- Families and the student see the note in Progress notes at once, labelled with the lesson name and Hong Kong date and time. A later class rename does not change it, corrections keep the link, and the database refuses to rewrite it.
- Notifications and teaching email work exactly as for other progress notes. Voice, picture and video attachments follow in stage 8b.

## Delivered: photos, voice and video (stage 8b of the mobile-first plan)

- Both the roster's **Share feedback** form and the student's Progress notes form take up to six attachments: photos or videos chosen from the phone (or taken with its camera) and voice recordings made in the browser with **Record voice**. A note may be only attachments.
- Each file is uploaded first to `POST /api/v1/students/{id}/note-media` as the raw body with its media type, then the note is shared with `mediaIds`. Photos (JPEG, PNG, WebP) up to 15 MB, recordings (WebM, Ogg, MP4, MP3) up to 25 MB, videos (MP4, MOV, WebM) up to 100 MB. The first bytes must match the declared type. Only an administrator or the swimmer's assigned coach may upload, an upload can be attached once, only by its uploader and only to that swimmer, and unattached uploads are deleted after 24 hours.
- Files are stored privately on the server (migration 33 records them in `note_media`; attached media can never change or be deleted). They are served only through `GET /api/v1/students/{id}/progress-notes/{noteId}/media/{mediaId}` to people who can read that swimmer's notes, with `nosniff`, a sandbox policy and range support so phones can play and seek video. Families see photos inline and play recordings and videos in place. Teaching email stays generic and never carries attachments.
- Corrections change the text only; a note with attachments may have its text cleared.
