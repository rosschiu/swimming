CREATE TABLE lesson_settings (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    parent_change_cutoff_hours integer NOT NULL DEFAULT 24 CHECK (parent_change_cutoff_hours BETWEEN 0 AND 8760),
    attendance_open_minutes integer NOT NULL DEFAULT 15 CHECK (attendance_open_minutes BETWEEN 0 AND 1440),
    correction_window_days integer NOT NULL DEFAULT 3 CHECK (correction_window_days BETWEEN 0 AND 365),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0)
);

INSERT INTO lesson_settings DEFAULT VALUES;
