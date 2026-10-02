import { FormEvent, useState } from "react";
import { Link, Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Building2, HeartHandshake } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";
import { createCreatorOrg, createMerchantOrg, type AccountKind } from "@/lib/orgApi";
import { getPendingBusinessName, getPendingDisplayName } from "@/lib/trackIntent";

const CreateOrg = ({ lockedKind }: { lockedKind?: AccountKind }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const trackParam = searchParams.get("track");
  const locked: AccountKind | null =
    lockedKind ?? (trackParam === "merchant" || trackParam === "creator" ? trackParam : null);
  const [kind, setKind] = useState<AccountKind>(locked ?? "merchant");
  const [name, setName] = useState("");
  const [businessName, setBusinessName] = useState(() => getPendingBusinessName());
  const [displayName, setDisplayName] = useState(() => getPendingDisplayName());
  const [handle, setHandle] = useState("");
  const [bio, setBio] = useState("");
  const [creating, setCreating] = useState(false);

  // One org per account — anyone who already holds one is sent back to
  // their apps instead of hitting a 409 here.
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => import("@/lib/orgApi").then((m) => m.listMyOrgs()), staleTime: 30_000 });
  const hasOrg = (orgsQuery.data ?? []).some((o) => o.status === "active");
  if (!orgsQuery.isLoading && hasOrg) {
    return <Navigate to="/merchant/apps" replace />;
  }

  const handleCreate = async (event: FormEvent) => {
    event.preventDefault();
    setCreating(true);
    const effectiveKind = locked ?? kind;
    try {
      const org =
        effectiveKind === "creator"
          ? await createCreatorOrg({
              name: name.trim(),
              display_name: displayName.trim(),
              handle: handle.trim().toLowerCase(),
              bio: bio.trim() || undefined,
            })
          : await createMerchantOrg({ name: name.trim(), business_name: businessName.trim() || undefined });
      toast.success(
        effectiveKind === "creator" ? "Creator page created — its sole owner is you. No team." : "Organization created — you are its owner.",
      );
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
      navigate(effectiveKind === "creator" ? `/onboarding/creator/${org.id}` : `/org/${org.id}/members`, { replace: true });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to create organization.");
    } finally {
      setCreating(false);
    }
  };

  const isCreator = (locked ?? kind) === "creator";

  return (
    <div className="flex min-h-[70vh] items-center justify-center px-4">
      <Card className="w-full max-w-md">
        <CardContent className="p-6">
          <div className="flex items-center gap-2">
            {isCreator ? <HeartHandshake className="h-5 w-5 text-slate-700" /> : <Building2 className="h-5 w-5 text-slate-700" />}
            <h1 className="text-lg font-bold text-slate-900">
              {isCreator ? "Create your creator page" : "Create your organization"}
            </h1>
          </div>
          <p className="mt-1 text-sm text-slate-500">
            {isCreator
              ? "A personal page where fans can support you. You are its sole owner — no team needed."
              : "Organizations own apps, API keys, and members. You will be its owner."}
          </p>

          {locked ? (
            <p className="mt-4 rounded-lg bg-slate-100 px-3 py-2 text-xs text-slate-500">
              {isCreator
                ? "Creator setup — personal account, no team. Need a business workspace instead? "
                : "Business setup — organization with team. Need a personal creator account instead? "}
              <Link
                to={isCreator ? "/onboarding/create-org?track=merchant" : "/onboarding/create-org?track=creator"}
                className="font-medium text-blue-600 hover:underline"
              >
                {isCreator ? "Go to Merchant setup →" : "Go to Creator setup →"}
              </Link>
            </p>
          ) : (
          <div className="mt-4 grid grid-cols-2 gap-2 rounded-lg bg-slate-100 p-1" role="tablist" aria-label="Account type">
            {(["merchant", "creator"] as AccountKind[]).map((k) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={kind === k}
                onClick={() => setKind(k)}
                className={`rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  kind === k ? "bg-white text-slate-900 shadow" : "text-slate-500 hover:text-slate-700"
                }`}
              >
                {k === "merchant" ? "Business" : "Creator"}
              </button>
            ))}
          </div>
          )}

          <form onSubmit={handleCreate} className="mt-5 space-y-4">
            <div>
              <label className="text-sm font-medium text-slate-700">
                {isCreator ? "Internal name" : "Organization name"}
              </label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                placeholder={isCreator ? "Amina's page (private)" : "Acme Ltd"}
                className="mt-1"
              />
              {isCreator && <p className="mt-1 text-xs text-slate-400">Private — fans see your display name instead.</p>}
            </div>
            {isCreator ? (
              <>
                <div>
                  <label className="text-sm font-medium text-slate-700">Display name</label>
                  <Input
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    required
                    maxLength={100}
                    placeholder="Amina Creates"
                    className="mt-1"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium text-slate-700">Handle (your support link)</label>
                  <div className="mt-1 flex items-center gap-1">
                    <span className="text-sm text-slate-400">lipago/c/</span>
                    <Input
                      value={handle}
                      onChange={(e) => setHandle(e.target.value.toLowerCase().replace(/[^a-z0-9._-]/g, ""))}
                      required
                      minLength={3}
                      maxLength={30}
                      placeholder="amina.creates"
                    />
                  </div>
                  <p className="mt-1 text-xs text-slate-400">3–30 lowercase letters, numbers, dots, hyphens or underscores. Permanent — contact support to change it later.</p>
                </div>
                <div>
                  <label className="text-sm font-medium text-slate-700">Bio <span className="font-normal text-slate-400">(optional)</span></label>
                  <Input value={bio} onChange={(e) => setBio(e.target.value)} maxLength={500} placeholder="I make videos about…" className="mt-1" />
                </div>
              </>
            ) : (
              <div>
                <label className="text-sm font-medium text-slate-700">Business name <span className="font-normal text-slate-400">(optional)</span></label>
                <Input value={businessName} onChange={(e) => setBusinessName(e.target.value)} placeholder="Acme Limited" className="mt-1" />
              </div>
            )}
            <Button type="submit" className="w-full" disabled={creating}>
              {creating ? "Creating..." : isCreator ? "Create creator page" : "Create organization"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
};

export default CreateOrg;
