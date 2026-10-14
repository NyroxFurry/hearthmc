// Thin fetch wrapper for the Hearth API.

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(method: Method, path: string, body?: unknown, raw = false): Promise<T> {
  const headers: Record<string, string> = { 'X-Hearth': '1' };
  let payload: BodyInit | undefined;
  if (body instanceof FormData || body instanceof Blob) {
    payload = body;
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await fetch(path, { method, headers, body: payload, credentials: 'same-origin' });
  } catch {
    throw new ApiError(0, 'Keine Verbindung zum Panel');
  }
  if (raw) return res as unknown as T;
  const text = await res.text();
  let data: any = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    if (res.status === 401 && onUnauthorized && !path.startsWith('/api/auth/')) onUnauthorized();
    throw new ApiError(res.status, data?.error || `Fehler ${res.status}`);
  }
  return data as T;
}

export const api = {
  get: <T = any>(path: string) => request<T>('GET', path),
  post: <T = any>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
  put: <T = any>(path: string, body?: unknown) => request<T>('PUT', path, body ?? {}),
  patch: <T = any>(path: string, body?: unknown) => request<T>('PATCH', path, body ?? {}),
  del: <T = any>(path: string, body?: unknown) => request<T>('DELETE', path, body),
};

export function qs(params: Record<string, string | number | boolean | undefined | null>): string {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') u.set(k, String(v));
  }
  const s = u.toString();
  return s ? '?' + s : '';
}

/** Upload files (optionally with relative paths for folder uploads) with progress. */
export function upload(
  url: string,
  files: { file: File; path?: string }[],
  onProgress?: (loaded: number, total: number) => void,
): Promise<any> {
  return new Promise((resolve, reject) => {
    const fd = new FormData();
    for (const f of files) {
      if (f.path) fd.append('relpath', f.path);
      fd.append('files', f.file, f.file.name);
    }
    const xhr = new XMLHttpRequest();
    xhr.open('POST', url);
    xhr.setRequestHeader('X-Hearth', '1');
    xhr.upload.onprogress = (e) => onProgress?.(e.loaded, e.total);
    xhr.onload = () => {
      let data: any = null;
      try {
        data = JSON.parse(xhr.responseText);
      } catch {}
      if (xhr.status >= 200 && xhr.status < 300) resolve(data);
      else reject(new ApiError(xhr.status, data?.error || `Upload fehlgeschlagen (${xhr.status})`));
    };
    xhr.onerror = () => reject(new ApiError(0, 'Upload fehlgeschlagen'));
    xhr.send(fd);
  });
}
