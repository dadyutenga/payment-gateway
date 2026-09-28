import { getAccessToken as readAccessToken } from "@/lib/auth";

type ApiEnvelope<T> = { data: T };
type ApiErrorEnvelope = { error?: { code?: string; message?: string; details?: Record<string, string> } };

const configuredApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
const defaultApiBaseUrl = import.meta.env.DEV ? "http://localhost:8080" : window.location.origin;
const apiBaseUrl = (configuredApiBaseUrl || defaultApiBaseUrl).replace(/\/$/, "");

export class SignupApiError extends Error {
  status: number;
  code?: string;
  details?: Record<string, string>;
  constructor(status: number, message: string, code?: string, details?: Record<string, string>) {
    super(message);
    this.name = "SignupApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

export type UserProfile = {
  id: string;
  email: string;
  is_admin: boolean;
  full_name?: string;
  email_verified: boolean;
  phone?: string;
  phone_verified: boolean;
};

export type OTPRequestResult = {
  channel: "email" | "sms";
  expires_at: string;
};

export type KYCSubmission = {
  org_id: string;
  business_name: string;
  tin: string;
  id_document_url: string;
  submitted_at: string;
  reviewed_by?: string;
  reviewed_at?: string;
  rejection_reason?: string;
};

export type KYCStatusResult = {
  submission: KYCSubmission;
  kyc_status: "pending" | "submitted" | "verified" | "rejected";
};

export type MeResult = {
  email: string;
  is_admin: boolean;
  email_verified: boolean;
  phone?: string;
  phone_verified: boolean;
  user?: UserProfile;
};

async function request<T>(path: string, options?: { method?: "GET" | "POST" | "PATCH" | "DELETE"; body?: unknown; auth?: boolean; formData?: FormData }): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (options?.auth !== false) {
    const token = readAccessToken();
    if (!token) throw new SignupApiError(401, "You need to sign in to continue.", "unauthorized");
    headers.Authorization = `Bearer ${token}`;
  }
  let body: BodyInit | undefined;
  if (options?.formData) {
    body = options.formData;
  } else if (options?.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(options.body);
  }
  const response = await fetch(`${apiBaseUrl}${path}`, { method: options?.method ?? "GET", headers, body });
  if (response.status === 204) return undefined as T;
  const contentType = response.headers.get("content-type") ?? "";
  const payload = contentType.includes("application/json") ? ((await response.json()) as ApiEnvelope<T> & ApiErrorEnvelope) : null;
  if (!response.ok) {
    throw new SignupApiError(response.status, payload?.error?.message || "The request could not be completed.", payload?.error?.code, payload?.error?.details);
  }
  return (payload as ApiEnvelope<T>).data;
}

export async function getMe() {
  return request<MeResult>("/api/v1/admin/me");
}

export async function requestOTP(input: { channel: "email" | "sms"; purpose: string; phone?: string }) {
  return request<OTPRequestResult>("/api/v1/auth/otp/request", { method: "POST", body: input });
}

export async function verifyOTP(input: { channel: "email" | "sms"; purpose: string; code: string }) {
  return request<{ verified: boolean }>("/api/v1/auth/otp/verify", { method: "POST", body: input });
}

export async function submitKYC(orgId: string, input: { business_name: string; tin: string; id_document_url: string }) {
  return request<KYCSubmission>(`/api/v1/orgs/${orgId}/kyc`, { method: "POST", body: input });
}

export async function getKYC(orgId: string) {
  return request<KYCStatusResult>(`/api/v1/orgs/${orgId}/kyc`);
}

export async function uploadKYCDocument(orgId: string, file: File) {
  const formData = new FormData();
  formData.append("document", file);
  return request<{ id_document_url: string }>(`/api/v1/orgs/${orgId}/kyc/document`, { method: "POST", formData });
}

export async function changePassword(input: { current_password: string; new_password: string }) {
  return request<{ changed: boolean }>("/api/v1/auth/password", { method: "POST", body: input });
}

export async function getOwnProfile() {
  return request<UserProfile>("/api/v1/auth/profile");
}

export async function updateOwnProfile(input: { full_name: string; phone: string }) {
  return request<UserProfile>("/api/v1/auth/profile", { method: "PATCH", body: input });
}
