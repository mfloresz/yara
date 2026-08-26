import type { ApiErrorPayload, V1CollectionMeta, V1Envelope } from "@/api/types";
import { clearAuth } from "@/app/auth";

export class ApiError extends Error {
  status: number;
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export type HttpClientConfig = {
  baseUrl: string;
};

// v1 responses are wrapped in {data, meta?, links?}. Auth responses
// (register/login/refresh/logout) and a few legacy-style endpoints stay
// outside the envelope; the unwrap helper detects envelopes by looking for
// the "data" key on a plain object, so non-enveloped responses are passed
// through unchanged.
function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isV1Envelope(value: unknown): value is V1Envelope {
  if (!isPlainObject(value)) return false;
  if ("error" in value) return true;
  // v1 canonical shape is {data, meta?, links?} — single resources have
  // only {data}. Collections add meta/links. Detect by the presence of
  // `data` at top level; no domain resource uses a top-level `data` key.
  if ("data" in value) return true;
  return false;
}

export function unwrapEnvelope<T>(body: unknown): T {
  if (isV1Envelope(body)) {
    return body.data as T;
  }
  return body as T;
}

// Collection responses add {meta,links} on top of {data}. Callers that need
// pagination (next page cursor / total count / has_more) should use this
// helper instead of unwrapping the envelope themselves.
export function unwrapCollection<T>(body: unknown): {
  data: T;
  meta: V1CollectionMeta | null;
  links: { self?: string; next?: string; prev?: string } | null;
} {
  if (isV1Envelope(body)) {
    return {
      data: body.data as T,
      meta: body.meta ?? null,
      links: body.links ?? null,
    };
  }
  return { data: body as T, meta: null, links: null };
}

export function createHttpClient(config: HttpClientConfig) {
  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const headers = new Headers(init?.headers ?? undefined);
    const isFormData =
      typeof FormData !== "undefined" && init?.body instanceof FormData;

    if (!isFormData && !headers.has("Content-Type") && init?.body) {
      headers.set("Content-Type", "application/json");
    }

    const response = await fetch(`${config.baseUrl}${path}`, {
      ...init,
      headers,
      credentials: "include",
    });

    if (!response.ok) {
      const payload = (await response
        .json()
        .catch(() => ({}))) as ApiErrorPayload;
      if (response.status === 401) {
        clearAuth();
      }
      throw new ApiError(
        payload.error?.message || payload.message || `HTTP ${response.status}`,
        response.status,
        payload.error?.code,
      );
    }

    if (response.status === 204) {
      return undefined as T;
    }

    const body = (await response.json()) as unknown;
    // Collections go through unwrapCollection, which needs the full envelope
    // {data, meta, links}. Callers type http.get<unknown> and call
    // unwrapCollection themselves, so we must not strip the envelope here.
    // Singles are {data: <resource>} where `data` is the resource itself —
    // automatically unwrapped above. Heuristic: envelope + caller used
    // `unknown` means they will unwrap; use T extends unknown check is not
    // possible at runtime, so detect by presence of `meta` on the wire.
    // All collection endpoints set `meta`; no single does.
    if (isPlainObject(body) && "data" in body && "meta" in body) {
      return body as T;
    }
    return unwrapEnvelope<T>(body);
  }

  async function downloadBlob(path: string): Promise<Blob> {
    const response = await fetch(`${config.baseUrl}${path}`, {
      credentials: "include",
    });

    if (!response.ok) {
      if (response.status === 401) {
        clearAuth();
      }
      throw new ApiError(`HTTP ${response.status}`, response.status);
    }

    return response.blob();
  }

  return {
    get: <T>(path: string) => request<T>(path),
    post: <T>(path: string, body?: BodyInit | object) =>
      request<T>(path, {
        method: "POST",
        body:
          body instanceof FormData || typeof body === "string"
            ? body
            : JSON.stringify(body ?? {}),
      }),
    put: <T>(path: string, body?: BodyInit | object) =>
      request<T>(path, {
        method: "PUT",
        body:
          body instanceof FormData || typeof body === "string"
            ? body
            : JSON.stringify(body ?? {}),
      }),
    patch: <T>(path: string, body?: BodyInit | object) =>
      request<T>(path, {
        method: "PATCH",
        body:
          body instanceof FormData || typeof body === "string"
            ? body
            : JSON.stringify(body ?? {}),
      }),
    delete: <T>(path: string) => request<T>(path, { method: "DELETE" }),
    downloadBlob: downloadBlob,
  };
}
