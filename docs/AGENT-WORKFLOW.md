# Goal-driven development with subagents

## Status and authorisation

The user has activated the web development goal and authorised subagent coordination, branch creation, pushes, PR creation and merges after passing checks and independent review. The goal excludes real payment integration, native clients and deployment. Every stage requires a PR and evidence; see BACKLOG.md.

## Lead responsibilities

Read DECISIONS.md and PLAN.md, inspect current workspace instructions and repository state, then translate the user's goal into concrete acceptance criteria. Resolve only questions that block the next dependent step. Continue independent authorised work while clarification is pending.

Maintain a short task/evidence log during implementation. The lead owns contracts, cross-domain decisions, integration, release boundaries and the final completion claim. Routine implementation choices should not repeatedly interrupt the user.

## Delegation model

Since 2026-11-18 Claude is the lead and also implements, reviews and tests each stage; the earlier `gpt-6.2-sol`/`gpt-6.2-astra` assignments are retired. Claude may use its own subagents for bounded, independent work, with explicit file ownership, and keeps integration and release responsibility. A separate review pass over the integrated diff (correctness, permissions, ledger effects, failure paths) precedes every merge and is recorded in the PR.

## Delivery loop

1. Define the next complete vertical slice and its checks before coding.
2. Agree interfaces and ownership.
3. Delegate independent bounded work; keep coupled work sequential.
4. Integrate changes and run the slice's checks against the combined result.
5. Review permissions, financial/credit effects, failure paths and user experience.
6. Resolve discovered issues before marking the slice complete.
7. Update decision and acceptance evidence; report meaningful progress and material uncertainty.

Worker completion is not product completion. Validate the integrated application, not only isolated unit tests. Do not label fixtures, mocked payment success or demo AI responses as working integrations.

## Completion report

Report what was delivered against the goal, checks and evidence, remaining limitations and any external dependencies. Mark a goal complete only when its agreed acceptance criteria are met. A web-only goal does not imply delivery of native apps or WeChat.

The lead should proceed autonomously within the user's scope. Do not send messages to third parties, publish externally, activate real payments or perform destructive data operations without applicable authorisation. Ask for missing business decisions rather than inventing consequential policies.
