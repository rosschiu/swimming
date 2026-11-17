import { useEffect, useRef, useCallback } from 'react';
import { request } from './api';

export type Translate = (en: string, zh: string) => string;
export type ProtectedProps = { csrfToken: string; expired: (error: unknown) => boolean; t: Translate };

// Unmounting a role/account view must stop its requests from updating the next view.
export function useProtectedRequest(expired: ProtectedProps['expired']) {
  const controllers = useRef(new Set<AbortController>());
  const expiredRef = useRef(expired);
  expiredRef.current = expired;
  useEffect(() => () => { controllers.current.forEach(controller => controller.abort()); controllers.current.clear(); }, []);
  return useCallback(async <T,>(path: string, options: RequestInit = {}): Promise<T> => {
    const controller = new AbortController();
    controllers.current.add(controller);
    try {
      const value = await request<T>(path, { ...options, signal: controller.signal });
      controller.signal.throwIfAborted();
      return value;
    } catch (error) {
      if (!controller.signal.aborted && expiredRef.current(error)) throw new DOMException('Session ended', 'AbortError');
      throw error;
    } finally { controllers.current.delete(controller); }
  }, []);
}

export const isAborted = (error: unknown) => error instanceof DOMException && error.name === 'AbortError';
