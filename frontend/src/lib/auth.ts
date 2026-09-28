const TOKEN_KEY = "payments_gateway_access_token";
const API_BASE = (import.meta.env.VITE_API_BASE_URL?.trim() || (import.meta.env.DEV ? "http://localhost:8080" : window.location.origin)).replace(/\/$/, "");

export function getAccessToken() { return localStorage.getItem(TOKEN_KEY); }
export function signOut() { localStorage.removeItem(TOKEN_KEY); }

export async function authenticate(mode: "login" | "register", email: string, password: string) {
  const response = await fetch(`${API_BASE}/api/v1/auth/${mode}`, {
    method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ email, password }),
  });
  const payload = await response.json().catch(() => null) as { data?: { access_token?: string }; error?: { message?: string } } | null;
  if (!response.ok || !payload?.data?.access_token) throw new Error(payload?.error?.message || "Unable to authenticate.");
  localStorage.setItem(TOKEN_KEY, payload.data.access_token);
}

// Admin-space login (POST /api/v1/admin/auth/login, aud=admin token).
// Interim: shares the same storage key until Phase 4 splits the admin and
// customer sessions apart — so only one space can stay signed in at a time.
export async function authenticateAdmin(email: string, password: string) {
  const response = await fetch(`${API_BASE}/api/v1/admin/auth/login`, {
    method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ email, password }),
  });
  const payload = await response.json().catch(() => null) as { data?: { access_token?: string }; error?: { message?: string } } | null;
  if (!response.ok || !payload?.data?.access_token) throw new Error(payload?.error?.message || "Unable to authenticate.");
  localStorage.setItem(TOKEN_KEY, payload.data.access_token);
}
