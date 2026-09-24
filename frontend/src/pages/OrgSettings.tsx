import { FormEvent, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import { deleteOrg, getOrg, updateOrg } from "@/lib/orgApi";

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

const KYC_COPY: Record<string, string> = {
  pending: "Not submitted — the org is limited until verification is complete.",
  submitted: "Under review — the org is limited until a decision is made.",
  verified: "Verified — full access.",
  rejected: "Rejected — see the reason below. Verification can be resubmitted in a later step.",
};

const OrgSettings = () => {
  const { orgId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000 });
  const org = orgQuery.data;
  const isOwner = org?.role === "owner";

  const [name, setName] = useState<string | null>(null);
  const [businessName, setBusinessName] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      await updateOrg(orgId, { name: (name ?? org?.name ?? "").trim(), business_name: (businessName ?? org?.business_name ?? "").trim() });
      toast.success("Organization updated.");
      queryClient.invalidateQueries({ queryKey: ["orgs", orgId] });
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update organization."));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!window.confirm(`Delete ${org?.name}? Only empty orgs (no apps) can be deleted.`)) return;
    setDeleting(true);
    try {
      await deleteOrg(orgId);
      toast.success("Organization deleted.");
      try {
        if (localStorage.getItem("payments_gateway_org_id") === orgId) {
          localStorage.removeItem("payments_gateway_org_id");
        }
      } catch {
        /* ignore */
      }
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
      navigate("/merchant/apps", { replace: true });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to delete organization."));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Settings — {org?.name ?? "…"}</h2>
        <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
          Organization settings.
          {org && <Badge variant="secondary">{org.role}</Badge>}
        </p>
      </div>

      {orgQuery.isLoading ? (
        <Skeleton className="mt-4 h-40 w-full" />
      ) : org ? (
        <>
          <Card className="mt-4">
            <CardContent className="flex items-start gap-3 p-4">
              <ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-amber-500" />
              <div className="text-sm">
                <p className="font-semibold text-slate-900">
                  Verification: {org.kyc_status}
                </p>
                <p className="mt-0.5 text-slate-500">{KYC_COPY[org.kyc_status] ?? org.kyc_status}</p>
                <p className="mt-1 text-xs text-slate-400">
                  Full KYC submission and review arrive in the next step — this banner already reflects live status.
                </p>
              </div>
            </CardContent>
          </Card>

          {isOwner ? (
            <Card className="mt-4">
              <CardContent className="p-4">
                <form onSubmit={handleSave} className="space-y-4">
                  <div>
                    <label className="text-sm font-medium text-slate-700">Organization name</label>
                    <Input value={name ?? org.name} onChange={(e) => setName(e.target.value)} required className="mt-1" />
                  </div>
                  <div>
                    <label className="text-sm font-medium text-slate-700">Business name</label>
                    <Input value={businessName ?? org.business_name ?? ""} onChange={(e) => setBusinessName(e.target.value)} className="mt-1" />
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <Button type="button" variant="outline" disabled={deleting} onClick={handleDelete}>
                      {deleting ? "Deleting..." : "Delete org"}
                    </Button>
                    <Button type="submit" disabled={saving}>{saving ? "Saving..." : "Save changes"}</Button>
                  </div>
                </form>
              </CardContent>
            </Card>
          ) : (
            <Card className="mt-4">
              <CardContent className="p-4 text-sm text-slate-500">
                Only owners can change settings or delete the organization.
              </CardContent>
            </Card>
          )}
        </>
      ) : (
        <p className="mt-4 text-sm text-slate-500">Organization not found.</p>
      )}
    </div>
  );
};

export default OrgSettings;
