# Stage 05c: teaching notifications

Based on verified main48a2c9b. Sol implements; Astra independently tests and reviews. Deliver persistent in-app teaching notifications, self-service email opt-in, durable SMTP dispatch and administrator failure handling. Booking reminders depend on actual bookings and remain required later. No historic backfill. No arbitrary real recipients: local Mailpit is the executable demo until named external demo recipients and SMTP access are supplied.

## Behaviour and access

Progress-note creation/correction and milestone events create notifications for all active currently linked guardian/self accounts in the same transaction as source/audit. Do not notify coaches/admins or the actor about their own administrative actions. Each source version creates at most one notification per recipient. Link changes never create past notifications. Store source references and times, not teaching text or student-name snapshots. Display generic event descriptions with currently authorized student names; opening the profile shows current source records, not implied original notification text.

Recipient ownership plus active current parent/student role and current matching guardian/self link govern list, unread count and mark-read. Unknown/inaccessible notification IDs return404. readAt is set once; repeated marking is idempotent. Linking again may restore in-app historical visibility but never revive suppressed emails.

One self-service user boolean controls teaching email, defaultfalse. Label 'Email shared teaching updates'; the only recipient is the user's login email. Server additionally requires that address in TEACHING_EMAIL_ALLOWLIST (comma-separated exact addresses normalized consistently with account email; empty disables teaching email). These controls do not affect invitation/recovery email. Creation-time opt-out/nonallowlist produces terminal suppressed delivery; future preference changes apply to future updates only.

Opt-out, relationship removal and account disable permanently suppress pending/retry deliveries in the same identity transaction. Leased deliveries are already authorized at claim commit and may complete; mark their withdrawal so a failed/expired attempt becomes suppressed rather than retried even after re-link/re-enable. Re-link/re-enable never unsuppresses mail. Retry claims always recheck current scope/preference/allowlist/address. Suppressed remains terminal. There is no current role/email-edit API; future role-edit operations must use the same suppression hook. Dispatch already checks current role/address.

Missing SMTP configuration is a retryable transport failure for opted-in, allowlisted recipients, not permanent policy suppression. The preference response still reports emailDeliveryAvailable=false until SMTP is configured. Retry budgets and administrator retry apply normally.

## Durable dispatch

Migration8 adds notifications, notification_deliveries and the user preference. A delivery is unique per notification; source notification uniqueness is recipient/kind/sourceId/sourceVersion. Kinds: progress.created, progress.updated, milestone.achieved, milestone.revoked; milestone source is immutable eventID. No destructive cascade of history.

State machine: pending → leased → sent/retry/failed/suppressed. Claim one due/expired-lease row in a short beginIdentity transaction with FOR UPDATE SKIP LOCKED, validate eligibility, increment lifetimeAttempts and cycleAttempts, assign unpredictable leaseToken, leaseUntil from database time, capture current eligible address, then commit. Perform SMTP outside all database locks. Whole SMTP attempt is bounded (existing dial5s + connected8s); use a60s lease. Finish only if token and leased status match. Expired lease at cycleAttempts>=5 becomes failed; otherwise reclaims count as another attempt. Retry delays after failures: 30s,60s,120s,240s; fifth attempt terminalfailed. Restart resumes due work; server starts/stops one worker loop with its lifecycle. No network under the global identity lock.

Email is generic bilingual text and APP_ORIGIN + /notifications, with no child name/ID, source ID, teaching text or capability token. Use stable notification-derived Message-ID. SMTP acceptance means sent, never inbox delivery. Crash after SMTP acceptance can duplicate delivery; at-least-once is explicit. Authorization linearizes at claim commit: an already-authorized generic mail may arrive after unlink; every app read still checks current scope. Store bounded sanitized failure categories rather than raw SMTP errors. Reuse existing SMTP transport with auth semantics unchanged.

Admin retry accepts failed state plus current delivery version and reason, resets cycleAttempts only, preserves lifetimeAttempts, rechecks eligibility and audits atomically. Concurrent retry gets409; sent/leased/suppressed cannot be retried. Attempt histories need not duplicate payload or raw errors; lifetime count/current status/timestamps and audited retries are retained.

## Bounded API/UI contract to finalize before parallel UI

- GET /api/v1/notifications: own scoped newest-first items, unreadCount, nextCursor; limit default25,max100; opaque validated (createdAt,id) cursor. Items: id,studentId,studentName,kind,sourceId,sourceVersion,createdAt,readAt nullable.
- PUT /api/v1/notifications/{notificationId}/read: scoped idempotent read marking, returns notification.
- GET /api/v1/notification-preferences and PUT same {emailEnabled:boolean}: own preference, parent/student only; response {emailEnabled,emailAddress,emailDeliveryAvailable} where available reflects configured SMTP and allowlist. Explain when enabled preference cannot currently send demo email.
- GET /api/v1/notification-deliveries: admin-only bounded cursor/status filter, no teaching content. Fields id,notificationId,recipientName,status,attempts,cycleAttempts,version,nextAttemptAt nullable,completedAt nullable,lastFailure nullable,createdAt; avoid exposing unnecessary email copies.
- POST /api/v1/notification-deliveries/{id}/retry: admin {version,reason}, returns updated delivery.

Family/student navigation gets Notifications with unread count, read state, explicit mark read, student-profile navigation, email toggle, bilingual loading/empty/failure states and safe account switching. /notifications must work from email, including arriving signed-out and going there after login. Admin gets a delivery queue/status filter and reasoned retry form with stale review. No new router dependency is needed solely for this path; preserve existing application navigation.

## Ownership and checks

Sol backend owns services/api, agreed OpenAPI/generated types, config/env examples. Sol frontend owns new notification components/App integration. Lead owns plan/evidence/integration. Astra owns tests and independent review. Backend sends finalized response schemas to frontend before interface-dependent work.

Acceptance: source/audit/outbox rollback; correction version and exact replay deduplication; multiple guardians/self recipient selection; current scope list/count/read; opt-out/unlink permanent pending suppression including unlink/relink before worker; no teachingcontent in mail; preference/allowlist/currentaddress boundaries; concurrent claims; crash recovery including fifth lease; stale-token completion; stable Message-ID; retries/terminal failure/admin concurrency and rollback; SMTP failures do not roll back already committed teaching updates; invitation/recovery regressions; real Mailpit email and cross-role browser journeys, direct signed-out /notifications link. All cumulative stage gates before merge. External real-demo delivery and booking reminders are not claimed complete here.
