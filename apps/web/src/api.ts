import type { components } from './generated/api';
export type User = components['schemas']['User'];
export type Session = components['schemas']['Auth'];
export type Student = components['schemas']['Student'];
export class APIError extends Error { constructor(public status: number, message: string, public code?: string, public details?: unknown) { super(message); } }
export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(`/api/v1${path}`, { ...options, credentials: 'same-origin', headers: { 'Content-Type': 'application/json', ...options.headers } });
  if (!response.ok) { const body = await response.json().catch(() => null); throw new APIError(response.status, body?.error?.message || 'Request failed. Please try again.', body?.error?.code, body?.error?.details); }
  return response.status === 204 ? undefined as T : response.json();
}
