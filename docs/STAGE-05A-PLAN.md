# Stage 05a: student progress notes

This complete vertical slice is brought forward while package expiry rules await clarification. It does not depend on bookings or payments. Skill milestones, lesson linking and voice dictation remain later requirements; none are claimed complete by notes alone.

## Behaviour and contract

Student profiles show persistent progress notes, newest first. Administrators and currently assigned coaches can add notes. Guardians and separately signed-in students can read only notes for students in their current scope. Notes are shared immediately; the editor explicitly explains family/student visibility. No private or draft notes are introduced.

The author coach may correct a note while still assigned; administrators may correct any note. Corrections require the current version and a reason. Store before/after audit history atomically. Preserve author identity and creation time; record last editor/time and version. No hard deletion is introduced.

- `GET /api/v1/students/{id}/progress-notes`: scoped `{items: ProgressNote[]}`.
- `POST /api/v1/students/{id}/progress-notes`: `{requestId, body}`. A trimmed nonempty body is limited to 4000 Unicode code points. Actor-scoped request IDs (1–128 ASCII characters) protect duplicate submission; reuse with another student/body returns 409. Creation returns 201; identical replay returns the current note with 200 after current permission checks.
- `PATCH /api/v1/students/{id}/progress-notes/{noteId}`: `{body, version, reason}`; trimmed reason 1–500 Unicode code points. Returns 200 or 409 for stale version. No silent overwrites.
- `ProgressNote`: `id`, `studentId`, `authorId`, `authorName`, `body`, `createdAt`, `updatedAt`, `updatedById`, `updatedByName`, `version`, `editable`. `editable` is calculated for the current viewer; permission checks remain server-side.

No observation-date field: timestamps describe recording/correction, not an inferred lesson date. Existing student/guardian/coach relationship semantics determine scope. Coordinate writes with identity mutations and recheck active account/role and assignment after locks; avoid a stale session authorising a write after unlink or disable. Reuse existing HTTP authentication, audit helpers and database transaction patterns.

## Ownership and validation

Lead owns OpenAPI and generated types, integration, stage evidence and PR/merge. Sol developer owns Go implementation/migration and React implementation. Astra owns independent tests and review. Agree the above interface before implementation.

Required evidence: coach creates feedback; guardian and student see it; unrelated family/coach denied; parent/student cannot write; other assigned coach cannot edit the author's note; author correction and admin correction preserve history; stale edits rejected; duplicate/concurrent creates produce one note/audit; failed audit rolls back; unlink/disable races obey current scope; browser switching students/accounts does not leak notes or drafts; bilingual loading/empty/error states preserve recoverable input. Cumulative backend/frontend/contract/Playwright checks and independent approval precede merge.
