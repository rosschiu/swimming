# Stage 02d evidence: lessons from availability windows

Status: [PR #6](https://github.com/rosschiu/swimming/pull/6) merged as `6e4c14221a0fdfa5daf19c786174a0edd4169133`. Tested head: `3db689c4f80ace3aeff38d93d88bce2f4e54d371`. Both CI runs passed; independent Sol review approved before merge. Main verified clean, identical to tested head, with contract drift check passing.

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
- OpenAPI types generated successfully; committed-drift check passed.
- Cumulative real-stack Playwright: 11 passed in 25.7 seconds. New journey schedules two lessons and confirms coach, parent and administrator schedule visibility after window cancellation. After the final focus improvement, both availability journeys passed again (10.1 seconds).
- First browser run failed because the persistent local test database had not received migration 5. Applied the migration and reran the entire suite successfully. CI already migrates before E2E.
- `git diff --check` passed.

## Review and delivery gate

Lead inspected shared scheduling validation, transaction boundaries, source-edit behaviour, retry matching and role scope. The user approved switching from unavailable `gpt-6.2-sol` to `gpt-6.1-sol`; independent reviewer stage02d_review_61 approved the tested head with no blocking findings. PR CI run 44147490241 and push CI run 44147476887 both passed.

After merge, the user refined role assignments to Sol developers and Astra reviewers/testers. Astra reviewer stage02d_astra_audit completed retrospective review and targeted verification: five PostgreSQL race tests and three frontend allocation tests passed; no blocking backend or permission defect found. It identified missing edit-form guidance. Sol implemented that correction and Astra independently tested/approved it in PR #7, merged as `c085d52`. See STAGE-02E-EVIDENCE.md.

No student booking, credit, payment or notification behaviour is added by this stage. Those remain in the original backlog. No public deployment or real messages were sent.
