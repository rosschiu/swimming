# Stage 05b: skill milestones

Based on verified main b88cc3d. Sol develops; Astra independently tests and reviews. This slice completes administrator-defined skills and student milestone history without introducing curriculum thresholds or automatic level promotion. Booking/payment decisions remain separate.

## Behaviour

Administrators create, edit and archive skill definitions (name 1–100 Unicode code points, description 0–1000). Catalogue edits require current version and reason. No hard deletion. Skill creation uses actor-scoped request IDs with original normalized inputs for exact replay.

Administrators and assigned coaches record achievements. Only the author of the current achievement while still assigned, or an administrator, can revoke it. Reasons are required for revocation (1–500 code points); achievement comments are optional (0–500). All feedback is immediately shared with the scoped student and guardians. Existing progress text/control-character validation applies, with explicit allowance for empty optional values.

Transitions: never recorded → achieved → revoked → achieved. Reject repeated states except exact request replay. Events are immutable, versioned per student/skill, and ordered by version. Archiving blocks new achievements, preserves existing achievements/history, and permits revocation. No invented assessment date: timestamps mean recording time.

Two tables: skills and milestone_events. Events include actor, state, reason, recorded time, per-student/skill version, request ID/original submission fields, skill definition version/name/description snapshots, and nullable reversed achievement ID. Unique actor/request ID and student/skill/version. Foreign keys do not cascade-delete history. Catalogue before/after and every event are audited atomically.

## API contract

- Admin-only GET /api/v1/skills → {items: Skill[]}, including archived definitions.
- Admin-only POST /api/v1/skills with {requestId,name,description} → Skill, 201 new/200 exact replay.
- Admin-only PATCH /api/v1/skills/{skillId} with {name,description,active,version,reason} → Skill; 409 stale version.
- Scoped GET /api/v1/students/{id}/milestones → {items: StudentMilestone[]}. Authorize student first; return all active skills plus only archived skills recorded for this student.
- Staff POST /api/v1/students/{id}/milestones/{skillId}/events with {requestId,state,version,skillVersion,reason} → MilestoneEvent, 201 new/200 exact replay. Both versions required: version=0 initially; skillVersion is the definition reviewed by the coach. For a new achievement it must match current catalogue version. Revocation ignores skillVersion for eligibility but preserves it in replay comparison.

Skill: id,name,description,active,version,createdAt,updatedAt.
MilestoneEvent: id,studentId,skillId,state,reason,version,skillVersion,skillName,skillDescription,actorId,actorName,recordedAt,reversesEventId (nullable).
StudentMilestone: skill (current definition), current (event or null), history (events newest version first), canAchieve,canRevoke.

An achievement snapshots the current reviewed definition. Revocation snapshots the achievement being reversed, including its definition version. Re-achievement snapshots the new reviewed definition. Do not expose replay keys/inputs. Sort skills by name then ID.

Use beginIdentity for access, catalogue writes and milestone writes. Recheck active account/current role and current student scope before replay. Exact replay lookup precedes current version/state/archive/transition checks and returns the original immutable event even after later events; changed student/skill/state/submitted versions/normalized reason conflicts. Disabled/unlinked users cannot replay successfully. Read-only catalogue access for coaches/families comes through scoped student endpoints.

## UI and ownership

Admin skill catalogue appears in school setup. Student profile embeds milestones keyed by student/account like progress notes. Show current definition separately from the snapshot assessed; distinguish archived skills. Read-only family/student views show status and history. Staff controls show consequences, required reasons, and the reviewed skill definition. Preserve recoverable input; lock uncertain submissions to exact retries. Reload authoritative state after mutation/replay, never replace current state with an older replayed event. Stale milestone/definition requires reload and explicit review before fresh submission. No new dependencies.

Sol backend owns services/api implementation/schema7/routes/migration registration and the agreed OpenAPI/generated types. Sol frontend owns skill catalogue and milestone components plus App integration. Lead reviews the contract and owns docs/integration. Astra owns Go/frontend/Playwright tests and independent review.

Acceptance: admin catalogue lifecycle; cross-role achieve/revoke/re-achieve; author/current-assignment permissions; stale definition/version; snapshots unchanged by edits; archive rules; exact retry after later events; conflicting actor-scoped retries; concurrent same-request and same-version submissions; atomic audit rollback; unlink/disable/role/catalogue races; scoped archived history; Unicode/control validation; bilingual loading/error/retry/draft isolation. Full cumulative stage gates before PR merge. No curriculum policy, actual lesson linking or AI functionality is claimed by this slice.
