import type { components } from './generated/api';
import type { Translate } from './useProtectedRequest';
export type Venue = components['schemas']['Venue'];
export type ClassType = components['schemas']['ClassType'];
export type ClassSession = components['schemas']['ClassSession'];
export type ClassSeries = components['schemas']['ClassSeries'];
export type AdminUser = components['schemas']['AdminUser'];
export type SchedulingSettings = components['schemas']['SchedulingSettings'];
export const regionNames = (t: Translate): Record<Venue['region'], string> => ({ 'hk-island': t('Hong Kong Island', '香港島'), 'nt-west-islands': t('NT West / Islands', '新界西／離島'), 'kowloon-east': t('Kowloon East', '九龍東'), 'nt-east': t('NT East', '新界東'), 'kowloon-west': t('Kowloon West', '九龍西') });
export function hkDate(value = new Date()) { return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Hong_Kong', year: 'numeric', month: '2-digit', day: '2-digit' }).format(value); }
export function hkTime(value: string) { return new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Hong_Kong', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(value)); }
export function rangeEnd(from: string) { const day = new Date(`${from}T12:00:00+08:00`); day.setUTCDate(day.getUTCDate() + 6); return hkDate(day); }

export function classLabel(value: Pick<ClassSession, 'startsAt' | 'endsAt' | 'classTypeName' | 'venueName' | 'coachName'>) {
  const day = hkDate(new Date(value.startsAt));
  const endDay = hkDate(new Date(value.endsAt));
  const end = day === endDay ? hkTime(value.endsAt) : `${endDay} ${hkTime(value.endsAt)}`;
  return `${day} ${hkTime(value.startsAt)}–${end} · ${value.classTypeName} · ${value.venueName} · ${value.coachName}`;
}

export function bookingCreditCovers(expiresOn: string | null, lesson: ClassSession, rule: "lesson_start" | "lesson_end") {
  if (!expiresOn) return true;
  const cutoff = Date.parse(`${expiresOn}T00:00:00+08:00`) + 86_400_000;
  if (Date.now() >= cutoff) return false;
  if (rule === 'lesson_start') return Date.parse(lesson.startsAt) < cutoff;
  const end = Date.parse(lesson.endsAt);
  // Date.parse truncates precision below a millisecond.
  return end < cutoff || (end === cutoff && !/\.\d{3}\d*[1-9]\d*(?:Z|[+-])/.test(lesson.endsAt));
}
