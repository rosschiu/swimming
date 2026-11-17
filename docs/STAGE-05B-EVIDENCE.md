# Stage 05b skill milestones: acceptance evidence

Merged [PR #9](https://github.com/rosschiu/swimming/pull/9) as `48a2c9b2eef0543979d5e0a824ba3bab96ddd666`, from tested head `9c89821a30a5c7a0abe69295ea51efce138d77d1`.

Sol prepared the catalogue/event API contract and implements backend and frontend; Astra independently reviewed the design and owns tests and final review. The design includes a skill-definition version check on achievement, immutable historical snapshots and exact event replay before current-state checks. The lead confirmed permissions follow progress-note correction ownership and recorded the contract in STAGE-05B-PLAN.md.

Initial lead contract compilation found that nullable current-event references were incompatible with the existing Ajv validator. Sol corrected the nullable schema; lead validated current=null with the existing validator and the production build passed. Tests must also cover recorded events without weakening the validator.

Lead source review covered scoped reads, current-role checks under the identity lock, replay ordering, preserved assessment snapshots, revocation ownership, atomic audits and frontend authoritative reload. Sol corrected timestamp defaults to clock_timestamp() so time spent waiting for the identity lock does not backdate newly recorded events. The persistent E2E database was still at migration 6 before this correction, so no applied migration was rewritten there.

The first cumulative frontend run passed 47 tests and failed two old school-setup tests because their fixtures did not include the new skills request. Astra corrected that fixture without weakening assertions. The next cumulative run passed all 54 frontend tests; targeted backend/PostgreSQL milestone tests passed in 5.995 seconds and the cross-role browser test passed in 9.3 seconds.

The browser run exposed duplicate sibling React keys for milestones and notes. Sol added distinct prefixes while retaining account/student identity; Astra added a console regression assertion and approved the corrected production source with no remaining blocking findings. Final cumulative checks passed: frontend54/build, Go/PostgreSQL race210.338s, Playwright13 in43.6s, vet and committed contract drift check. Both CI runs passed (push44155124839, PR44155185991). Merged main was clean, identical to the tested tree, and contracts:check passed again. No representative scale or manual screen-reader testing is claimed.
