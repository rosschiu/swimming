# Stage 02d evidence: lessons from availability windows

Status: implementation under validation; independent review and merge pending.

## Behaviour

Administrators can schedule multiple shorter lessons from a submitted availability window. Coach and venue come from the current window. The server checks window version/status/bounds, active references, coach conflicts and travel time in the shared scheduling transaction. Proposed capacity prefills the form; administrators set the actual lesson capacity.

Migration 5 stores source links, original window snapshots and actor-scoped request IDs. Identical retries return the existing class without another audit event. Different input with the same key is rejected. Window edits/cancellation leave scheduled classes unchanged, as explicitly confirmed by the user. Ordinary class corrections preserve the source relationship. Coach source-history reads also check current class assignment.

The UI preserves input after definitive failures and locks an uncertain submission for a safe retry. Source classes are displayed with current details and an explanation of the historical relationship. Keyboard focus moves into the opened panel. English and Traditional Chinese text is supplied.

## Local evidence

- Frontend: final 40 tests passed, including focus, conflict preservation and uncertain retry behaviour.
- Final production build passed.
- Two new PostgreSQL integration tests passed with `-race`: lifecycle/scope and concurrent retry/rollback (12.057 seconds including race overhead).
- Additional lock-wait cancellation test passed with PostgreSQL and `-race` (6.150 seconds).
- Full Go/PostgreSQL race suite passed (136.540 seconds); the subsequently added lock-wait test also passed separately as recorded above. `go vet ./...` passed.
- OpenAPI types generated successfully; final committed-drift check pending.
- Cumulative real-stack Playwright: 11 passed in 25.7 seconds. New journey schedules two lessons and confirms coach, parent and administrator schedule visibility after window cancellation.
- First browser run failed because the persistent local test database had not received migration 5. Applied the migration and reran the entire suite successfully. CI already migrates before E2E.
- `git diff --check` passed.

## Review and delivery gate

Lead inspected shared scheduling validation, transaction boundaries, source-edit behaviour, retry matching and role scope. Requested an independent `gpt-6.2-sol` reviewer; the tool rejected the model. No substitute model has been used. This is not independent approval, and the stage must not merge until required review and CI pass.

No student booking, credit, payment or notification behaviour is added by this stage. Those remain in the original backlog. No public deployment or real messages were sent.
