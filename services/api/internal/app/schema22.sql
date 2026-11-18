CREATE INDEX lesson_change_pending_target ON lesson_change_requests(target_class_id, requested_at DESC, id DESC) WHERE status='pending';
