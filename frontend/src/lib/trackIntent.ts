import type { AccountKind } from "@/lib/orgApi";

const KIND_KEY = "lipago_pending_account_kind";
const BUSINESS_KEY = "lipago_pending_business_name";
const DISPLAY_KEY = "lipago_pending_display_name";

export function setTrackIntent(kind: AccountKind) {
  try {
    localStorage.setItem(KIND_KEY, kind);
  } catch {
    /* storage unavailable — intent stays in URL params */
  }
}

export function getTrackIntent(): AccountKind | null {
  try {
    const v = localStorage.getItem(KIND_KEY);
    return v === "merchant" || v === "creator" ? v : null;
  } catch {
    return null;
  }
}

export function clearTrackIntent() {
  try {
    localStorage.removeItem(KIND_KEY);
  } catch {
    /* ignore */
  }
}

export function setPendingBusinessName(v: string) {
  try {
    sessionStorage.setItem(BUSINESS_KEY, v);
  } catch {
    /* ignore */
  }
}

export function getPendingBusinessName(): string {
  try {
    return sessionStorage.getItem(BUSINESS_KEY) ?? "";
  } catch {
    return "";
  }
}

export function setPendingDisplayName(v: string) {
  try {
    sessionStorage.setItem(DISPLAY_KEY, v);
  } catch {
    /* ignore */
  }
}

export function getPendingDisplayName(): string {
  try {
    return sessionStorage.getItem(DISPLAY_KEY) ?? "";
  } catch {
    return "";
  }
}

export function clearPendingNames() {
  try {
    sessionStorage.removeItem(BUSINESS_KEY);
    sessionStorage.removeItem(DISPLAY_KEY);
  } catch {
    /* ignore */
  }
}
