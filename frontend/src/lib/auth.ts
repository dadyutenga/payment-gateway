const CUSTOMER_TOKEN_KEY = "payments_gateway_customer_token";
const ADMIN_TOKEN_KEY = "payments_gateway_admin_token";
const API_BASE = (import.meta.env.VITE_API_BASE_URL?.trim() || (import.meta.env.DEV ? "http://localhost:8080" : window.location.origin)).replace(/\/$/, "");

export function getCustomerToken() { return localStorage.getItem(CUSTOMER_TOKEN_KEY); }
export function getAdminToken() { return localStorage.getItem(ADMIN_TOKEN_KEY); }
export function getAccessToken() { return getCustomerToken() || getAdminToken(); }

export function signOutCustomer() { localStorage.removeItem(CUSTOMER_TOKEN_KEY); }
export function signOutAdmin() { localStorage.removeItem(ADMIN_TOKEN_KEY); }
export function signOut() { signOutCustomer(); signOutAdmin(); }

export async function authenticate(mode: "login" | "register", email: string, password: string) {
  return authenticateTrack(undefined, mode, email, password);
}

export async function authenticateTrack(
  kind: "merchant" | "creator" | undefined,
  mode: "login" | "register",
  email: string,
  password: string,
  profile?: { display_name?: string; handle?: string; bio?: string },
) {
  const prefix = kind
    ? `/api/v1/${kind === "creator" ? "individual" : kind}/auth`
    : "/api/v1/auth";
  const response = await fetch(`${API_BASE}${prefix}/${mode}`, {
    method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ email, password, ...(profile || {}) }),
  });
  const payload = await response.json().catch(() => null) as { data?: { access_token?: string }; error?: { message?: string } } | null;
  if (!response.ok || !payload?.data?.access_token) throw new Error(payload?.error?.message || "Unable to authenticate.");
  localStorage.setItem(CUSTOMER_TOKEN_KEY, payload.data.access_token);
}

export async function authenticateAdmin(email: string, password: string) {
  const response = await fetch(`${API_BASE}/api/v1/admin/auth/login`, {
    method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ email, password }),
  });
  const payload = await response.json().catch(() => null) as { data?: { access_token?: string }; error?: { message?: string } } | null;
  if (!response.ok || !payload?.data?.access_token) throw new Error(payload?.error?.message || "Unable to authenticate.");
  localStorage.setItem(ADMIN_TOKEN_KEY, payload.data.access_token);
}
