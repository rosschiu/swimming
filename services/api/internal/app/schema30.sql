ALTER TABLE lesson_settings ADD COLUMN reminder_hours integer NOT NULL DEFAULT 24 CHECK (reminder_hours BETWEEN 0 AND 168);
ALTER TABLE lesson_settings ADD COLUMN reminder_email boolean NOT NULL DEFAULT true;
-- One row per reminded occurrence: an approved move to another class or time is a new occurrence.
CREATE TABLE booking_reminders (
    booking_id text NOT NULL REFERENCES bookings(id),
    sequence integer NOT NULL CHECK (sequence > 0),
    class_id text NOT NULL REFERENCES class_sessions(id),
    starts_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (booking_id, sequence),
    UNIQUE (booking_id, class_id, starts_at)
);
CREATE TRIGGER immutable_booking_reminders BEFORE UPDATE OR DELETE ON booking_reminders FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN ('progress.created','progress.updated','milestone.achieved','milestone.revoked','booking.created','lesson_change.requested','lesson_change.approved','lesson_change.rejected','booking.cancelled','class.cancelled','class.coach_changed','booking.reminder'));
