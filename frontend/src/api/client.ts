import { t } from '../i18n';

export class ApiError extends Error {
  readonly status: number;
  readonly data: unknown;

  constructor(message: string, status: number, data: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.data = data;
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE';

let csrfToken = '';
let onUnauthorized: (() => void) | null = null;

export function setCsrfToken(token: string): void {
  csrfToken = token;
}

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler;
}

function isErrorPayload(value: unknown): value is { error: string } {
  return typeof value === 'object' && value !== null && 'error' in value
    && typeof (value as { error: unknown }).error === 'string';
}

/** Request body as is, with its own Content-Type (e.g. a lab YAML file). */
export class RawBody {
  constructor(readonly text: string, readonly contentType: string) {}
}

export async function request<T>(
  method: Method,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const headers: Record<string, string> = {};
  const init: RequestInit = { method, credentials: 'same-origin', headers };
  if (body instanceof RawBody) {
    headers['Content-Type'] = body.contentType;
    init.body = body.text;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  if (method !== 'GET') headers['X-CSRF-Token'] = csrfToken;
  if (signal) init.signal = signal;

  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 30_000);
  init.signal = signal ?? controller.signal;
  try {
    const response = await fetch(path, init);
    const text = await response.text();
    let data: unknown = null;
    if (text) {
      try {
        data = JSON.parse(text) as unknown;
      } catch {
        data = text;
      }
    }
    if (!response.ok) {
      if (response.status === 401 && onUnauthorized) onUnauthorized();
      const message = isErrorPayload(data)
        ? data.error
        : (typeof data === 'string' && data ? data : response.statusText);
      throw new ApiError(message || `HTTP ${response.status}`, response.status, data);
    }
    return data as T;
  } finally {
    window.clearTimeout(timeout);
  }
}

export async function requestText(path: string, signal?: AbortSignal): Promise<string> {
  const response = await fetch(path, { credentials: 'same-origin', ...(signal ? { signal } : {}) });
  const text = await response.text();
  if (!response.ok) {
    if (response.status === 401 && onUnauthorized) onUnauthorized();
    throw new ApiError(text || response.statusText, response.status, text);
  }
  return text;
}

export async function requestBlob(path: string): Promise<Blob> {
  const response = await fetch(path, { credentials: 'same-origin' });
  if (!response.ok) {
    if (response.status === 401 && onUnauthorized) onUnauthorized();
    throw new ApiError(response.statusText || t('Download failed'), response.status, null);
  }
  return response.blob();
}

export function uploadFile<T>(
  path: string,
  file: Blob,
  onProgress?: (loaded: number) => void,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', path);
    xhr.setRequestHeader('X-CSRF-Token', csrfToken);
    if (onProgress) xhr.upload.onprogress = (event) => onProgress(event.loaded);
    xhr.onload = () => {
      let data: unknown = null;
      try {
        data = JSON.parse(xhr.responseText) as unknown;
      } catch {
        data = null;
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(data as T);
        return;
      }
      if (xhr.status === 401 && onUnauthorized) onUnauthorized();
      const message = isErrorPayload(data) ? data.error : `${t('Download failed')} (${xhr.status})`;
      reject(new ApiError(message, xhr.status, data));
    };
    xhr.onerror = () => reject(new ApiError(t('Network error while uploading'), 0, null));
    xhr.send(file);
  });
}

/**
  * Uploads the image as is. The Content-Type is taken from the file: the server accepts
  * PNG, JPEG, WEBP and SVG and validates the content by signature itself.
 */
export async function uploadImage(path: string, file: Blob): Promise<unknown> {
  const response = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': file.type || 'application/octet-stream', 'X-CSRF-Token': csrfToken },
    body: file,
  });
  const text = await response.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text) as unknown;
    } catch {
      data = text;
    }
  }
  if (!response.ok) {
    if (response.status === 401 && onUnauthorized) onUnauthorized();
    throw new ApiError(isErrorPayload(data) ? data.error : response.statusText, response.status, data);
  }
  return data;
}
