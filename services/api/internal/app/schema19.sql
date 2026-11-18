ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (
    kind IN ('progress.created','progress.updated','milestone.achieved','milestone.revoked','booking.created',
             'lesson_change.approved','lesson_change.rejected')
);
