-- A note may be only a photo, recording or video, so its text can be empty.
ALTER TABLE progress_notes DROP CONSTRAINT progress_notes_body_check, DROP CONSTRAINT progress_notes_original_body_check;
ALTER TABLE progress_notes ADD CONSTRAINT progress_notes_body_check CHECK (length(body) <= 4000),
    ADD CONSTRAINT progress_notes_original_body_check CHECK (length(original_body) <= 4000);
CREATE TABLE note_media (
    id text PRIMARY KEY,
    student_id text NOT NULL REFERENCES students(id),
    uploader_id text NOT NULL REFERENCES users(id),
    note_id text REFERENCES progress_notes(id),
    position integer CHECK (position BETWEEN 0 AND 5),
    kind text NOT NULL CHECK (kind IN ('image','audio','video')),
    content_type text NOT NULL CHECK (length(content_type) BETWEEN 1 AND 100),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 1 AND 104857600),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((note_id IS NULL) = (position IS NULL)),
    UNIQUE (note_id, position)
);
CREATE INDEX note_media_unattached ON note_media(created_at) WHERE note_id IS NULL;
-- Unattached uploads may be attached once or cleaned up; attached media never changes.
CREATE FUNCTION note_media_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.note_id IS NOT NULL THEN
        RAISE EXCEPTION 'attached note media is immutable';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF (NEW.id, NEW.student_id, NEW.uploader_id, NEW.kind, NEW.content_type, NEW.size_bytes, NEW.sha256, NEW.created_at)
        IS DISTINCT FROM (OLD.id, OLD.student_id, OLD.uploader_id, OLD.kind, OLD.content_type, OLD.size_bytes, OLD.sha256, OLD.created_at) THEN
        RAISE EXCEPTION 'note media is immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER note_media_fixed BEFORE UPDATE OR DELETE ON note_media FOR EACH ROW EXECUTE FUNCTION note_media_fixed();
