ALTER TABLE progress_notes
    ADD COLUMN booking_id text REFERENCES bookings(id),
    ADD COLUMN class_id text REFERENCES class_sessions(id),
    ADD COLUMN lesson_name text,
    ADD COLUMN lesson_starts_at timestamptz,
    ADD CONSTRAINT progress_notes_lesson CHECK (
        (booking_id IS NULL) = (class_id IS NULL) AND (booking_id IS NULL) = (lesson_name IS NULL) AND (booking_id IS NULL) = (lesson_starts_at IS NULL)
    );
CREATE INDEX progress_notes_booking ON progress_notes(booking_id) WHERE booking_id IS NOT NULL;
CREATE FUNCTION progress_note_lesson_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.booking_id IS DISTINCT FROM OLD.booking_id OR NEW.class_id IS DISTINCT FROM OLD.class_id
        OR NEW.lesson_name IS DISTINCT FROM OLD.lesson_name OR NEW.lesson_starts_at IS DISTINCT FROM OLD.lesson_starts_at THEN
        RAISE EXCEPTION 'progress note lesson is immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER progress_note_lesson_fixed BEFORE UPDATE ON progress_notes FOR EACH ROW EXECUTE FUNCTION progress_note_lesson_fixed();
