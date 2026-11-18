# Staff rehearsal with the synthetic swimming school

Run this walkthrough in a disposable local environment with the 20-swimmer demo. Follow [Local development](DEVELOPMENT.md#start), substituting the [synthetic school seed](DEVELOPMENT.md#seed-the-synthetic-demo-school) for the minimal seed. Use the same database for the API and seed. Leave outbound mail disabled or use local mail capture. The shared demo can contain other visitors’ activity; use a fresh local seed when comparing exact counts.

Use English for the labels below. Sign out between roles or give each role its own browser profile. Sign in with the [synthetic credentials](DEVELOPMENT.md#start). The four sessions are School Administrator (`admin@swim.test`), Coach Alex (`coach@swim.test`), Amy’s parent (`parent@swim.test`), and Amy (`student@swim.test`).

All dates are Hong Kong dates. Let **W** be the Monday of the week when the seed first ran. In **Audit history**, enter `demo-school-v1` in **Filter by record ID** and select **Apply filter**. Expand **Change details** on the `demo.school.seeded` entry to read `weekOf`. For example, if W is 16 November 2026, use 9, 23 and 30 November for Amy’s three bookings. The seed keeps its initial dates on replay.

## Administrator

1. Open **Students** and confirm there are 20 students. Search for Amy and open her profile. She is a Beginner. Her credit history and booked lessons should agree with the guardian view below.
2. Choose **Schedule** to open **Class schedule**. Set **From date** and **To date** to W minus seven days, choose **Harbour Pool** in **Filter venue**, and select **Apply filters**. On **Water Confidence**, 17:00–18:00, open **View roster**.
3. Find Amy, marked **Present**, with **Recorded by** showing Coach Alex. The recorded time is 18:00 on that date. The fixture writes attendance evidence without an individual audit event, so this row has no **View attendance audit** link. On a row marked through the roster in the app, that link opens audit history filtered to the booking.

## Coach Alex

1. Choose **Schedule** to open **Your teaching schedule** and load the date W minus seven days. Open **View roster** for **Water Confidence** at **Harbour Pool**. Check that seven swimmers are present, including Amy, Anson Wong and Daisy Ng. Garden Pool and Seaside Pool are outside this coach’s assignment.
2. Load the Harbour Pool class at W plus seven days. The roster has the same seven swimmers with unmarked attendance on the base seed. The mark control only opens at the configured class-relative time. Review this state without submitting a mark.

## Amy’s guardian

1. Open **My children**, select **Amy**, and find **Booked lessons**. Set **Bookings from date** to W minus seven days and **Bookings to date** to W plus fourteen days, then select **Load booked lessons**. Expect three Harbour Pool Mondays at 17:00: the first is **Present** and the next two are **Unmarked**. The school’s class catalogue lists offered lessons. The profile’s booked lesson list shows Amy’s reservations.
2. In **Credit history**, confirm five available, two reserved, one consumed and zero expired for a fresh, unexpired seed. Expiry is W plus 42 days, inclusive in Hong Kong. The two reserved credits correspond to the unmarked bookings. The consumed credit corresponds to the present booking.
3. Open **Packages and payments**, select Amy, and under **Purchase history** find `demo-purchase-amy`. Choose **View simulated receipt**. Confirm **Demo 8-lesson package**, eight credits and HKD 1,200, labelled as simulated. Read Amy’s seeded note under **Progress notes** on her student profile. It describes floating and controlled breathing with a kickboard.

## Amy’s student account

1. In **My profile**, open **Amy**. Repeat the **Booked lessons** range and check the three bookings, attendance and progress note match the guardian’s view.
2. Verify this account has no booking controls or purchase history. Try opening the student profile path `/students/ben` while signed in as Amy. It should be unavailable.

## Reconcile the rehearsal

| Record | Fresh base fixture |
| --- | --- |
| Student records | 20 swimmers, each with a mock purchase, simulated receipt and progress note |
| Timetable | 15 occurrences across three five-week series, one each at Harbour Pool, Garden Pool and Seaside Pool |
| Reservations and attendance | 60 booking records, 20 marked present and 40 with no attendance mark |
| Amy’s package | 8 issued = 5 available + 2 reserved + 1 consumed + 0 expired |

These totals assume only the base school seed. The [optional lesson-change history](DEVELOPMENT.md#add-the-demo-lesson-change-examples) moves Anson’s reservation and adds a class. When rehearsing with old data, load the historical date range and account for expiry before comparing the balance. Replaying the seed preserves visitor changes and does not advance the timetable.

## Optional practice write

Only on the disposable local copy, sign in as administrator and create a student named **Practice Swimmer** at level **Beginner**. Verify that the administrator can reload the new profile and that the record is absent from Amy’s parent and student directories. Leave the practice profile unlinked. The administrator directory now shows 21 students, and the original booking, credit and receipt expectations still hold. Prepare a fresh disposable database to repeat the baseline exercise.

## Support and escalation

- For a missing record, confirm the signed-in role, current student link or coach assignment, and Hong Kong date range. Use **Reload roster** or **Reload credit history** after checking the filters. For a submitted booking with an uncertain result, use [Retry same booking](DEVELOPMENT.md#exercise-flexible-booking-with-synthetic-accounts) with the original choices.
- For a mismatch, have the administrator compare the booked lesson, roster, credit movements, and booking-specific audit history. Capture the role, Hong Kong timestamp, affected synthetic record IDs, steps, and expected and observed result. Share these through the team’s private issue process. Keep credentials and real student information out of screenshots and reports.
- For an unavailable local app, the operator checks the API readiness endpoint `/api/v1/ready` and the API logs. For the designated demo host, follow the [VM delivery runbook](VM-DELIVERY.md). Rehearse recovery separately using the [synthetic restore procedure](RESTORE.md).
