import { FormEvent, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";
import {
  enableSupportPage,
  getSupportSettings,
  updateSupportSettings,
  type Organization,
} from "@/lib/orgApi";
import { listMyApps } from "@/lib/merchantApi";

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

export type SupportLinkDraft = { label: string; amount_mode: "fixed" | "open"; amount?: string };

// Support-page editor shared by Settings (Support page tab) and My Page:
// enable flow, receiving app, amount bounds, and buttons (max 6).
const SupportPageEditor = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const settingsQuery = useQuery({
    queryKey: ["orgs", org.id, "support-settings"],
    queryFn: () => getSupportSettings(org.id).catch(() => null),
    staleTime: 15_000,
  });
  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const settings = settingsQuery.data;
  const enabled = !!settings?.support_app_id;

  const [appId, setAppId] = useState<string | null>(null);
  const [minAmount, setMinAmount] = useState<string | null>(null);
  const [maxAmount, setMaxAmount] = useState<string | null>(null);
  const [links, setLinks] = useState<SupportLinkDraft[] | null>(null);
  const [saving, setSaving] = useState(false);
  const [enabling, setEnabling] = useState(false);

  const effAppId = appId ?? settings?.support_app_id ?? "";
  const effMin = minAmount ?? settings?.min_amount ?? "";
  const effMax = maxAmount ?? settings?.max_amount ?? "";
  const effLinks = links ?? (settings?.links ?? []).map((l) => ({ label: l.label, amount_mode: l.amount_mode, amount: l.amount ?? "" }));

  const handleEnable = async () => {
    setEnabling(true);
    try {
      await enableSupportPage(org.id);
      toast.success("Support page enabled — share your link.");
      queryClient.invalidateQueries({ queryKey: ["orgs", org.id, "support-settings"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to enable the support page."));
    } finally {
      setEnabling(false);
    }
  };

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      await updateSupportSettings(org.id, {
        support_app_id: effAppId || undefined,
        min_amount: effMin.trim() || undefined,
        max_amount: effMax.trim() || undefined,
        links: effLinks.map((l) => ({ label: l.label.trim(), amount_mode: l.amount_mode, amount: l.amount?.trim() || undefined })),
      });
      toast.success("Support page saved.");
      queryClient.invalidateQueries({ queryKey: ["orgs", org.id, "support-settings"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to save the support page."));
    } finally {
      setSaving(false);
    }
  };

  const updateLink = (i: number, patch: Partial<SupportLinkDraft>) => {
    setLinks((effLinks.map((l, j) => (j === i ? { ...l, ...patch } : l))));
  };

  if (settingsQuery.isLoading) {
    return <p className="mt-3 text-sm text-slate-500">Loading…</p>;
  }
  if (!isOwner) {
    return <p className="mt-3 text-sm text-slate-500">Only owners can change the support page.</p>;
  }
  if (!enabled) {
    return (
      <div className="mt-4">
        <p className="text-sm text-slate-600">Your page shows your profile, but fans can’t pay you until you enable it — enabling creates a dedicated receiving app and two starter buttons.</p>
        <Button className="mt-3" disabled={enabling} onClick={handleEnable}>{enabling ? "Enabling…" : "Enable support page"}</Button>
      </div>
    );
  }
  return (
    <form onSubmit={handleSave} className="mt-4 space-y-4">
      <div>
        <label className="text-sm font-medium text-slate-700">Receiving app</label>
        <select value={effAppId} onChange={(e) => setAppId(e.target.value)} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-2 text-sm" required>
          <option value="" disabled>Select an app…</option>
          {(appsQuery.data ?? []).map((a) => (
            <option key={a.id} value={a.id}>{a.name}</option>
          ))}
        </select>
        <p className="mt-1 text-xs text-slate-400">Support payments settle into this app’s balance.</p>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label className="text-sm font-medium text-slate-700">Minimum amount (TZS)</label>
          <Input value={effMin} onChange={(e) => setMinAmount(e.target.value.replace(/[^\d]/g, ""))} inputMode="numeric" placeholder="500" className="mt-1" />
          <p className="mt-1 text-xs text-slate-400">Floor is 500 TZS against dust.</p>
        </div>
        <div>
          <label className="text-sm font-medium text-slate-700">Maximum amount (TZS)</label>
          <Input value={effMax} onChange={(e) => setMaxAmount(e.target.value.replace(/[^\d]/g, ""))} inputMode="numeric" placeholder="Live tier cap" className="mt-1" />
          <p className="mt-1 text-xs text-slate-400">Clamped to your live per-transaction tier at order time.</p>
        </div>
      </div>
      <div>
        <label className="text-sm font-medium text-slate-700">Support buttons (max 6)</label>
        <div className="mt-2 space-y-2">
          {effLinks.map((l, i) => (
            <div key={i} className="flex flex-col gap-2 rounded-lg border border-slate-200 p-2 sm:flex-row sm:items-center">
              <Input value={l.label} onChange={(e) => updateLink(i, { label: e.target.value })} maxLength={60} placeholder="Buy me coffee" className="flex-1" />
              <select value={l.amount_mode} onChange={(e) => updateLink(i, { amount_mode: e.target.value as "fixed" | "open" })} className="h-10 rounded-md border border-slate-300 px-2 text-sm">
                <option value="open">Custom amount</option>
                <option value="fixed">Fixed amount</option>
              </select>
              {l.amount_mode === "fixed" && (
                <Input value={l.amount ?? ""} onChange={(e) => updateLink(i, { amount: e.target.value.replace(/[^\d]/g, "") })} inputMode="numeric" placeholder="5000" className="w-28" />
              )}
              <Button type="button" size="sm" variant="outline" onClick={() => setLinks(effLinks.filter((_, j) => j !== i))}>Remove</Button>
            </div>
          ))}
        </div>
        {effLinks.length < 6 && (
          <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => setLinks([...effLinks, { label: "", amount_mode: "open" }])}>
            Add button
          </Button>
        )}
      </div>
      <label className="flex items-start gap-3 rounded-lg border border-slate-200 px-3 py-2.5 opacity-60">
        <input type="checkbox" checked={false} disabled className="mt-1 h-4 w-4" />
        <span>
          <span className="block text-sm font-medium text-slate-800">Public supporters wall (coming soon)</span>
          <span className="block text-xs text-slate-500">Supporter messages stay private in your dashboard until this ships. Default: off.</span>
        </span>
      </label>
      <Button type="submit" disabled={saving}>{saving ? "Saving…" : "Save support page"}</Button>
    </form>
  );
};

export default SupportPageEditor;
