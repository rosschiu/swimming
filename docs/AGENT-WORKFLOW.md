# Goal-driven development with subagents

## Status and authorisation

The user has activated the web development goal and authorised subagent coordination, branch creation, pushes, PR creation and merges after passing checks and independent review. The goal excludes real payment integration, native clients and deployment. Every stage requires a PR and evidence; see BACKLOG.md.

## Lead responsibilities

Read DECISIONS.md and PLAN.md, inspect current workspace instructions and repository state, then translate the user's goal into concrete acceptance criteria. Resolve only questions that block the next dependent step. Continue independent authorised work while clarification is pending.

Maintain a short task/evidence log during implementation. The lead owns contracts, cross-domain decisions, integration, release boundaries and the final completion claim. Routine implementation choices should not repeatedly interrupt the user.

## Delegation model

Use the user-selected `gpt-6.2-sol` for coding and `gpt-6.2-astra` for independent testing and review. Explicitly select the model for each delegated task. If a model is unavailable, report the tool rejection. The lead retains integration and release responsibility.

With the current four-agent concurrency limit, use the lead plus up to three focused workers. Example assignments:

| Worker | Bounded responsibility |
|---|---|
| Backend | Go services, database changes and relevant domain/integration tests |
| Frontend | React screens and client tests using agreed API contracts |
| Verification or AI | Independent review/test work, or a bounded assistant integration once dependencies are ready |

Roles may change per phase. Do not run agents merely to occupy slots. Independent frontend/backend work begins after agreeing the contract; migrations and shared contract edits have one owner. Review consequential work independently when useful.

Every delegated task states: objective, allowed paths, dependencies, acceptance criteria, required checks, exclusions and expected handoff. Use isolated branches/worktrees where supported; otherwise fence file ownership explicitly. Never have multiple agents edit the same files concurrently without coordination.

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
