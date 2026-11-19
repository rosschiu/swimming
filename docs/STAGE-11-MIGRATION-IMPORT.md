# Migration import from Excel

Stage 11 of the mobile-first plan. Confirmed by the owner on 2026-11-18: the import's purpose is migrating an existing school's records, it accepts Excel (.xlsx), and remaining credit balances arrive as one "Migrated balance" package per swimmer with the imported credits and expiry, an audit entry, and no receipt or payment record.

## Proposed sheet layout (column mapping)

The owner asked for a column mapping proposal; this is what is built. One row per swimmer on the first worksheet. The first non-empty row holds headings, in English or Traditional Chinese, in any order. Other columns are ignored and listed in the check, so an old system's IDs or notes can stay in the sheet.

| Heading | 中文標題 | Required | Rule |
|---|---|---|---|
| Student name | 學員姓名 | Yes | 1–100 characters |
| Level | 程度 | Yes | 1–100 characters |
| Guardian name | 家長姓名 | For a new guardian | 1–100 characters; ignored for an existing guardian account |
| Guardian email | 家長電郵 | No | Matches an existing guardian account, or creates one |
| Second guardian name | 第二位家長姓名 | For a new guardian | As above |
| Second guardian email | 第二位家長電郵 | No | As above |
| Credits | 剩餘堂數 | No | Whole number 0–1000; 0 or blank means no balance |
| Credits expire on | 堂數到期日 | With credits | `YYYY-MM-DD` text or an Excel date; today or later |

Administrators download a template from **Accounts › Import**.

## Behaviour

- **Check, then import.** Choosing a file checks it without writing anything: totals, ignored columns and every problem by row number. The import button appears only for a sheet with no problems. Importing sends the file again with the hash of the checked file; a different file is refused, and if anything changed since the check (for example, another administrator created an account), the page checks again and shows the new results.
- **All or nothing, once.** One transaction creates every swimmer, guardian link, guardian account and balance, or nothing. The same file imported again returns the first result instead of creating copies (`imports.file_sha256` is unique).
- **Guardians.** The same email on several rows is one account linked to each sibling; email matching ignores case. An email that belongs to a coach, administrator or student account is refused. A new guardian account is created inactive, without a password, and with an invitation shown as **Not sent yet**. Nobody is emailed during an import; an administrator sends each invitation from **Accounts › Invitations** when ready.
- **Duplicates.** The same swimmer and guardians twice in the sheet, or a swimmer name already linked to that existing guardian, is refused.
- **Balances.** Remaining credits become a "Migrated balance" package under the current school credit policy, recorded like a complimentary grant (`credit_grants.kind = 'migrated'`, linked to the import), so it books, expires, cancels, returns and recovers like any other credits. It creates no purchase, payment or receipt and is not offered as a paid renewal package. Families and staff see it labelled **Migrated balance**.
- **Audit.** `import.completed` with totals and file hash, plus `student.imported` per swimmer and `invitation.imported` per new guardian.
- **Limits.** 5 MB and 2,000 swimmers per file. The reader is built in (no third-party dependency) and bounds every part it decompresses.

## Not included

Student sign-in accounts, coaches, classes, schedules, past attendance and payment history are not imported. Row problems are reported in English only. CSV is not accepted. Check the demo's reverse proxy allows 5 MB uploads during rollout.
