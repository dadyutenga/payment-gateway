import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";
import {
  getCreatorSurvey,
  saveCreatorSurvey,
  type CreatorSurvey,
  type Organization,
} from "@/lib/orgApi";

export const CREATOR_CATEGORIES = [
  { value: "content_creator", label: "Content creator" },
  { value: "musician_artist", label: "Musician / artist" },
  { value: "freelancer_consultant", label: "Freelancer / consultant" },
  { value: "coach_educator", label: "Coach / educator" },
  { value: "nonprofit_cause", label: "Nonprofit / cause" },
  { value: "personal_use", label: "Personal use / receiving from friends and family" },
  { value: "other", label: "Other" },
] as const;

export const CREATOR_REFERRALS = [
  { value: "social_media", label: "Social media" },
  { value: "friend_colleague", label: "Friend or colleague" },
  { value: "search_engine", label: "Search engine" },
  { value: "event_conference", label: "Event or conference" },
  { value: "advertisement", label: "Advertisement" },
  { value: "other", label: "Other" },
] as const;

export const CREATOR_USE_CASES = [
  { value: "support_tips", label: "Receive support / tips from fans" },
  { value: "digital_products", label: "Sell digital products / services" },
  { value: "freelance_work", label: "Get paid for freelance work" },
  { value: "api_integration", label: "API integration for my own app" },
] as const;

export const CREATOR_VOLUME_BANDS = [
  { value: "under_100k", label: "Under TZS 100,000" },
  { value: "100k_1m", label: "TZS 100,000 – 1,000,000" },
  { value: "1m_10m", label: "TZS 1,000,000 – 10,000,000" },
  { value: "10m_100m", label: "TZS 10,000,000 – 100,000,000" },
  { value: "over_100m", label: "Over TZS 100,000,000" },
] as const;

export const CREATOR_TXN_BANDS = [
  { value: "under_50", label: "Under 50" },
  { value: "50_200", label: "50 – 200" },
  { value: "200_1000", label: "200 – 1,000" },
  { value: "1000_5000", label: "1,000 – 5,000" },
  { value: "over_5000", label: "Over 5,000" },
] as const;

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

// Shared individual onboarding survey form (Part 2). Used by the onboarding
// page (first run) and the Settings survey tab (editing). Display name
// (Q1) saves onto the org row with the same submit.
const CreatorSurveyForm = ({
  org,
  initial,
  submitLabel,
  onSaved,
}: {
  org: Organization;
  initial?: CreatorSurvey | null;
  submitLabel: string;
  onSaved: (survey: CreatorSurvey) => void;
}) => {
  const [displayName, setDisplayName] = useState(org.display_name ?? "");
  const [category, setCategory] = useState(initial?.category ?? "");
  const [categoryOther, setCategoryOther] = useState(initial?.category_other ?? "");
  const [referral, setReferral] = useState(initial?.referral_source ?? "");
  const [useCases, setUseCases] = useState<string[]>(initial?.use_cases ?? []);
  const [volume, setVolume] = useState(initial?.expected_volume_band ?? "");
  const [txn, setTxn] = useState(initial?.expected_txn_band ?? "");
  const [saving, setSaving] = useState(false);

  const showDisplayName = !initial;
  const effectiveDisplayName = showDisplayName ? displayName : (org.display_name ?? "");
  const wantsAPI = useCases.includes("api_integration");

  const toggleUseCase = (value: string) => {
    setUseCases((prev) => (prev.includes(value) ? prev.filter((u) => u !== value) : [...prev, value]));
  };

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      const survey = await saveCreatorSurvey(org.id, {
        display_name: showDisplayName ? displayName.trim() || undefined : undefined,
        category,
        category_other: category === "other" ? categoryOther.trim() : "",
        referral_source: referral,
        use_cases: useCases,
        expected_volume_band: volume,
        expected_txn_band: txn,
      });
      toast.success("Answers saved.");
      onSaved(survey);
    } catch (err) {
      toast.error(errorMessage(err, "Unable to save your answers."));
    } finally {
      setSaving(false);
    }
  };

  const selectClass = "mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-2 text-sm";

  return (
    <form onSubmit={handleSubmit} className="space-y-5">
      {showDisplayName && (
        <div>
          <label className="text-sm font-medium text-slate-700">1. Display name</label>
          <p className="mt-0.5 text-xs text-slate-400">How you will appear on your public support page.</p>
          <Input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            maxLength={100}
            placeholder={org.display_name || "Amina Creates"}
            className="mt-1"
          />
        </div>
      )}

      <div>
        <label className="text-sm font-medium text-slate-700">{showDisplayName ? "2." : "1."} What best describes you?</label>
        <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
          {CREATOR_CATEGORIES.map((c) => (
            <label
              key={c.value}
              className={`cursor-pointer rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                category === c.value ? "border-slate-900 bg-slate-900 text-white" : "border-slate-200 hover:border-slate-400"
              }`}
            >
              <input type="radio" name="category" value={c.value} checked={category === c.value} onChange={() => setCategory(c.value)} className="sr-only" />
              {c.label}
            </label>
          ))}
        </div>
        {category === "other" && (
          <Input
            value={categoryOther}
            onChange={(e) => setCategoryOther(e.target.value)}
            maxLength={200}
            placeholder="Tell us what you do…"
            className="mt-2"
          />
        )}
      </div>

      <div>
        <label className="text-sm font-medium text-slate-700">{showDisplayName ? "3." : "2."} How did you hear about LipaGO?</label>
        <select value={referral} onChange={(e) => setReferral(e.target.value)} className={selectClass} required>
          <option value="" disabled>Select one…</option>
          {CREATOR_REFERRALS.map((r) => (
            <option key={r.value} value={r.value}>{r.label}</option>
          ))}
        </select>
      </div>

      <div>
        <label className="text-sm font-medium text-slate-700">{showDisplayName ? "4." : "3."} What do you plan to use LipaGO for? <span className="font-normal text-slate-400">(pick all that apply)</span></label>
        <div className="mt-2 space-y-2">
          {CREATOR_USE_CASES.map((u) => (
            <label key={u.value} className="flex cursor-pointer items-start gap-3 rounded-lg border border-slate-200 px-3 py-2.5">
              <input
                type="checkbox"
                checked={useCases.includes(u.value)}
                onChange={() => toggleUseCase(u.value)}
                className="mt-1 h-4 w-4 accent-slate-900"
              />
              <span className="text-sm text-slate-800">{u.label}</span>
            </label>
          ))}
        </div>
        {wantsAPI && (
          <div className="mt-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs text-amber-900">
            Building your own app with our API? The <strong>business / developer track</strong> fits better (API keys,
            webhooks, team roles). Business and creator accounts are fully separate —{" "}
            <Link to="/merchant/register" className="font-medium underline">
              create a separate business account
            </Link>
            .
          </div>
        )}
      </div>

      <div>
        <label className="text-sm font-medium text-slate-700">{showDisplayName ? "5." : "4."} Expected monthly amount received</label>
        <select value={volume} onChange={(e) => setVolume(e.target.value)} className={selectClass} required>
          <option value="" disabled>Select a range…</option>
          {CREATOR_VOLUME_BANDS.map((b) => (
            <option key={b.value} value={b.value}>{b.label}</option>
          ))}
        </select>
      </div>

      <div>
        <label className="text-sm font-medium text-slate-700">{showDisplayName ? "6." : "5."} Expected monthly number of payments</label>
        <select value={txn} onChange={(e) => setTxn(e.target.value)} className={selectClass} required>
          <option value="" disabled>Select a range…</option>
          {CREATOR_TXN_BANDS.map((b) => (
            <option key={b.value} value={b.value}>{b.label}</option>
          ))}
        </select>
        <p className="mt-1 text-xs text-slate-400">
          Rough estimates are fine — they help us set safe starting defaults. They never raise your live limits on their own.
        </p>
      </div>

      {!showDisplayName && effectiveDisplayName === "" && (
        <p className="text-xs text-amber-700">Tip: set your display name under General first — it shows on your public page.</p>
      )}

      <Button type="submit" className="w-full" disabled={saving}>
        {saving ? "Saving…" : submitLabel}
      </Button>
    </form>
  );
};

export async function loadCreatorSurvey(orgId: string): Promise<CreatorSurvey | null> {
  try {
    return await getCreatorSurvey(orgId);
  } catch {
    return null;
  }
}

export default CreatorSurveyForm;
