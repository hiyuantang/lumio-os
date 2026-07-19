// SPDX-License-Identifier: AGPL-3.0-only
import type { ProtocolErrorCode, WireError } from './protocol';

export const API_BASE = '/api/v1';

export class ApiError extends Error {
  readonly code: ProtocolErrorCode;
  readonly details: Record<string, unknown>;
  readonly status: number | null;

  constructor(code: ProtocolErrorCode, message: string, details: Record<string, unknown> = {}, status: number | null = null) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.details = details;
    this.status = status;
  }
}

export function csrfToken(): string | null {
  const match = document.cookie.match(/(?:^|;\s*)lumio_csrf=([^;]+)/);
  return match?.[1] ? decodeURIComponent(match[1]) : null;
}

type QueryParams = Record<string, string | number | undefined>;

const MAX_ATTEMPTS: Partial<Record<ProtocolErrorCode, number>> = {
  unavailable: 3,
  internal: 2,
  busy: 5,
};
const NETWORK_ATTEMPTS = 3;

function backoffMs(err: ApiError, attempt: number): number {
  const hinted = Number(err.details.retryAfterMs);
  const base = Number.isFinite(hinted) && hinted > 0 ? hinted : 500 * 2 ** (attempt - 1);
  return Math.min(8000, base);
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms);
  });
}

async function rawRequest<T>(method: string, path: string, params?: QueryParams, body?: unknown): Promise<T> {
  const query = params
    ? Object.entries(params).filter((entry): entry is [string, string | number] => entry[1] !== undefined)
    : [];
  const url = query.length > 0 ? `${API_BASE}${path}?${new URLSearchParams(query.map(([k, v]) => [k, String(v)]))}` : `${API_BASE}${path}`;
  const headers: Record<string, string> = {};
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    const csrf = csrfToken();
    if (csrf) headers['X-Lumio-CSRF'] = csrf;
  }
  let res: Response;
  try {
    res = await fetch(url, {
      method,
      headers,
      credentials: 'same-origin',
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError('unavailable', 'The server is unreachable.');
  }
  let parsed: { ok?: boolean; data?: T; error?: WireError };
  try {
    parsed = (await res.json()) as { ok?: boolean; data?: T; error?: WireError };
  } catch {
    throw new ApiError('internal', `Unexpected response from the server (HTTP ${res.status}).`, {}, res.status);
  }
  if (parsed.ok === true) return parsed.data as T;
  if (parsed.error && typeof parsed.error.code === 'string') {
    throw new ApiError(parsed.error.code, parsed.error.message || 'The request failed.', parsed.error.details ?? {}, res.status);
  }
  throw new ApiError('internal', `Unexpected response from the server (HTTP ${res.status}).`, {}, res.status);
}

export async function apiGet<T>(path: string, params?: QueryParams): Promise<T> {
  let attempt = 0;
  for (;;) {
    try {
      return await rawRequest<T>('GET', path, params);
    } catch (err) {
      attempt += 1;
      const apiErr = err instanceof ApiError ? err : new ApiError('unavailable', 'The server is unreachable.');
      const maxAttempts = err instanceof ApiError ? (MAX_ATTEMPTS[apiErr.code] ?? 1) : NETWORK_ATTEMPTS;
      if (attempt >= maxAttempts) throw apiErr;
      await delay(backoffMs(apiErr, attempt));
    }
  }
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  return rawRequest<T>('POST', path, undefined, body);
}
