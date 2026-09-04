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

// v1 wraps every response in {data, meta?, links?}. Single resources are
// {data: <resource>}; collections add meta/links on top. Both helpers below
// assume the envelope is always present; callers that fetch a collection
// (and need meta/links) call `unwrapCollection`, while single-resource
// fetches let the request function auto-unwrap to `data`.
function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isV1Envelope(value: unknown): value is V1Envelope {
  if (!isPlainObject(value)) return false;
  if ("error" in value) return true;
  return "data" in value;
}

export function unwrapEnvelope<T>(body: unknown): T {
  if (isV1Envelope(body)) {
    return body.data as T;
  }
  return body as T;
}

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
      if (response.status === 403 && payload.error?.code === "account_blocked") {
        clearAuth();
      }
      throw new ApiError(
        payload.error?.message ?? `HTTP ${response.status}`,
        response.status,
        payload.error?.code,
      );
    }

    if (response.status === 204) {
      return undefined as T;
    }

    const body = (await response.json()) as unknown;
    // Collections come back as {data, meta, links} — keep the full envelope
    // so callers can pass it to `unwrapCollection` and read meta. Single
    // resources come back as {data: <resource>} — strip to `data`.
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

  // DELETE with JSON body, for endpoints like admin user delete with a
  // transfer target. fetch supports bodies on DELETE; the plain delete()
  // helper above does not send one.
  async function deleteWithBody<T>(path: string, body: object): Promise<T> {
    const response = await fetch(`${config.baseUrl}${path}`, {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      body: JSON.stringify(body),
    });
    if (!response.ok) {
      const payload = (await response
        .json()
        .catch(() => ({}))) as ApiErrorPayload;
      if (response.status === 401) {
        clearAuth();
      }
      if (response.status === 403 && payload.error?.code === "account_blocked") {
        clearAuth();
      }
      throw new ApiError(
        payload.error?.message ?? `HTTP ${response.status}`,
        response.status,
        payload.error?.code,
      );
    }
    if (response.status === 204) {
      return undefined as T;
    }
    const responseBody = (await response.json()) as unknown;
    return unwrapEnvelope<T>(responseBody);
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
    deleteWithBody: deleteWithBody,
    downloadBlob: downloadBlob,
  };
}
