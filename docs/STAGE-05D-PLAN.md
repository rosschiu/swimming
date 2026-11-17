# Stage 05d: browser navigation for existing role workflows

Existing screens use transient React state: most navigation pushes `/`, and student profiles have no stable address. Users cannot reliably bookmark, refresh or return through browser history to their work. This slice gives existing persistent workflows ordinary browser navigation; it does not substitute for pending booking, finance, attendance or AI features.

## Accepted implementation scope

Use a small explicit route representation and path parser/builder, with no router dependency. Paths: `/students`, `/students/{id}`, `/schedule`, `/availability`, `/accounts`, `/setup`, `/audit`, `/notification-deliveries`, and existing `/notifications`. `/` remains a compatible directory entry. The URL is authoritative on startup and `popstate`; do not trust `history.state` for view or access.

Keep invitation and reset routes outside ordinary routing, capture their token before stripping the query, and preserve their existing behavior even with a current session. Ordinary routes survive sign-in/session expiry; account records and drafts do not survive account replacement or sign-out. Store no records or tokens in history/local storage.

Gate each protected route by current role before mounting its component or issuing its requests. Render a bilingual unavailable page for role/path mismatches. Fetch profiles through the existing scoped endpoint; missing/inaccessible records share a generic state. Safely decode one ID segment and encode IDs when building API URLs. Include unknown/malformed route, loading, unavailable and retry states.

Navigation controls are native anchors: sidebar, student cards, directory return and notification profile links. Intercept only ordinary unmodified primary clicks. Preserve modified clicks, new tabs, keyboard access and aria-current. Move focus to the destination heading/main after navigation; do not steal focus during routine refreshes or validation errors.

Guard asynchronous profile loads and save success/error/finally with session and route/request generations. Clear old profile data when changing IDs. A late response must not restore an old profile, clear another request's busy state or expire a replacement session. A navigation away does not imply server-side save rollback. Editors remain local to directory/profile routes; navigation exits editing, cancel returns to the underlying route, and creation navigates to its saved profile only while its originating context remains current.

## Ownership and acceptance

Sol owns App.tsx and minimal route/helper/style/Notifications changes. Astra owns independent tests and review. Lead owns integration, plan/evidence and releases. Existing backend contracts and permissions remain authoritative; no domain or policy changes.

Tests cover direct entry/reload/sign-in restoration for every allowed route; role mismatches with no protected component requests; profile missing/unauthorized/malformed/unknown/retry and bilingual states; Back/Forward; native modified clicks and keyboard focus; notification-to-profile; edits/create/cancel/conflict; late profile/save responses, stale401 and account replacement; invitation/reset stripping and completion. Real-stack Playwright exercises admin schedule refresh, coach availability, parent profile/notification navigation and denied profile access, alongside cumulative journeys. Reuse existing test fixtures and SPA fallback.

Cumulative frontend/build, Go/PostgreSQL race, contracts, real-stack Playwright and CI must pass with independent review before merge. This plan was reviewed by Astra before implementation. Material package and AI decisions remain pending and are not inferred from silence.
