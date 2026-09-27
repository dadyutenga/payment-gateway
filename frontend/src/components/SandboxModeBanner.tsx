import { useQuery } from "@tanstack/react-query";
import { ShieldAlert } from "lucide-react";
import { Link } from "react-router-dom";
import { listMyOrgs } from "@/lib/orgApi";

// SandboxModeBanner shows when the signed-in user's active org is not
// KYC-verified: live API keys and live payments are blocked until review.
const SandboxModeBanner = () => {
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000, retry: false });
  const orgs = (orgsQuery.data ?? []).filter((o) => o.status === "active");
  if (orgs.length === 0) return null;

  let activeOrgId = "";
  try {
    activeOrgId = localStorage.getItem("payments_gateway_org_id") ?? "";
  } catch {
    /* ignore */
  }
  const active = orgs.find((o) => o.id === activeOrgId) ?? orgs[0];
  if (!active || active.kyc_status === "verified") return null;

  const copy =
    active.kyc_status === "submitted"
      ? "Verification is under review. Sandbox mode only until an admin approves it."
      : active.kyc_status === "rejected"
        ? "Verification was rejected. Resubmit documents to unlock live payments."
        : "Your organization is not verified — you're in sandbox mode. Submit verification to enable live API keys and payments.";

  return (
    <div className="mb-4 flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
      <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
      <div>
        <p className="font-medium">Sandbox mode · {active.name}</p>
        <p className="mt-0.5 text-amber-800">{copy}</p>
        <Link to={`/onboarding/kyc/${active.id}`} className="mt-1 inline-block font-medium text-amber-950 underline">
          Open verification →
        </Link>
      </div>
    </div>
  );
};

export default SandboxModeBanner;
