ALTER TABLE lesson_change_requests DROP CONSTRAINT lesson_change_requests_status_check;
ALTER TABLE lesson_change_requests ADD CONSTRAINT lesson_change_requests_status_check CHECK (status IN ('pending','approved','rejected','withdrawn'));
