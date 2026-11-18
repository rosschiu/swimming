import type { components } from './generated/api';
import type { Translate } from './useProtectedRequest';

type Attendance = components['schemas']['Attendance'];

export function outcomeLabel(status: Attendance['status'], t: Translate) {
  return { present: t('Present', '已出席'), absent: t('Absent', '缺席'), ill: t('Ill', '病假') }[status];
}

export function creditLabel(outcome: Attendance['creditOutcome'], t: Translate) {
  return { consumed: t('credit used', '已使用堂數'), retained: t('credit returned', '已退回堂數'), expired: t('credit returned after expiry', '堂數退回時已過期') }[outcome];
}

// Families see the current outcome and where the credit went, never staff correction reasons.
export function attendanceSummary(attendance: Attendance | undefined, t: Translate) {
  return attendance ? `${outcomeLabel(attendance.status, t)} · ${creditLabel(attendance.creditOutcome, t)}` : t('Unmarked', '尚未記錄');
}
