import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import {
  getIndividualAccount,
  resolveIndividualLogoSrc,
  updateIndividualAccount,
  uploadIndividualLogo,
  type Organization,
} from "@/lib/orgApi";
import SharePanel from "@/components/SharePanel";
import SupportPageEditor from "@/components/SupportPageEditor";

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

// Individual "My Page": edit handle/bio/photo/links in one place plus the
// share panel. Same APIs and validation as the Settings tabs — this
// screen composes them for the simplified individual nav.
const MyPageProfile = ({ account, isOwner }: { account: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const [displayName, setDisplayName] = useState(account.display_name ?? "");
  const [handle, setHandle] = useState(account.handle ?? "");
  const [bio, setBio] = useState(account.bio ?? "");
  const [primaryColor, setPrimaryColor] = useState(account.primary_color || "#0f172a");
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [logoSrc, setLogoSrc] = useState("");

  useEffect(() => {
    let cancelled = false;
    let objectUrl = "";
    (async () => {
      try {
        const src = await resolveIndividualLogoSrc(account.logo_url);
        if (!cancelled) {
          if (objectUrl) URL.revokeObjectURL(objectUrl);
          if (src.startsWith("blob:")) objectUrl = src;
          setLogoSrc(src);
        } else if (src.startsWith("blob:")) {
          URL.revokeObjectURL(src);
        }
      } catch {
        if (!cancelled) setLogoSrc("");
      }
    })();
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [account.logo_url]);

  const dirty =
    displayName !== (account.display_name ?? "") ||
    handle !== (account.handle ?? "") ||
    bio !== (account.bio ?? "") ||
    primaryColor !== (account.primary_color || "#0f172a");

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      await updateIndividualAccount({
        name: account.name,
        display_name: displayName.trim() || undefined,
        handle: handle.trim().toLowerCase() || undefined,
        bio: bio.trim() || undefined,
        primary_color: primaryColor.trim() || undefined,
      });
      toast.success("Page updated.");
      queryClient.invalidateQueries({ queryKey: ["individual", "account"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update your page."));
    } finally {
      setSaving(false);
    }
  };

  const handlePhoto = async (file: File | undefined) => {
    if (!file) return;
    if (file.size > 2 << 20) {
      toast.error("Photo must be under 2MB.");
      return;
    }
    if (!/^(image\/jpeg|image\/png|image\/webp)$/.test(file.type)) {
      toast.error("Photo must be a JPEG, PNG, or WEBP image.");
      return;
    }
    setUploading(true);
    try {
      await uploadIndividualLogo(file);
      toast.success("Photo uploaded.");
      queryClient.invalidateQueries({ queryKey: ["individual", "account"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to upload photo."));
    } finally {
      setUploading(false);
    }
  };

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <div className="flex items-center gap-4">
          {logoSrc ? (
            <img src={logoSrc} alt="" className="h-16 w-16 rounded-full object-cover" />
          ) : (
            <span className="flex h-16 w-16 items-center justify-center rounded-full bg-slate-200 text-2xl font-bold text-slate-500">
              {(displayName || "L").slice(0, 1)}
            </span>
          )}
          <div>
            <h3 className="text-sm font-bold text-slate-800">Profile</h3>
            <p className="text-xs text-slate-500">What supporters see at <span className="font-mono">/c/{account.handle || "…"}</span></p>
            <label className="mt-1 inline-block cursor-pointer text-xs text-blue-600 hover:underline">
              {uploading ? "Uploading…" : "Change photo (JPEG/PNG/WEBP, ≤2MB)"}
              <Input
                type="file"
                accept="image/jpeg,image/png,image/webp"
                disabled={!isOwner || uploading}
                className="hidden"
                onChange={(e) => { void handlePhoto(e.target.files?.[0]); e.target.value = ""; }}
              />
            </label>
          </div>
        </div>
        <form onSubmit={handleSave} className="mt-4 space-y-4">
          <div>
            <label className="text-sm font-medium text-slate-700">Display name</label>
            <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} disabled={!isOwner} maxLength={100} placeholder="Amina" className="mt-1" />
          </div>
          <div>
            <label className="text-sm font-medium text-slate-700">Handle</label>
            <Input value={handle} onChange={(e) => setHandle(e.target.value.toLowerCase().replace(/[^a-z0-9._-]/g, ""))} disabled={!isOwner} minLength={3} maxLength={30} placeholder="amina.creates" className="mt-1" />
            <p className="mt-1 text-xs text-slate-400">3–30 lowercase letters, numbers, dots, hyphens or underscores.</p>
          </div>
          <div>
            <label className="text-sm font-medium text-slate-700">Bio</label>
            <Input value={bio} onChange={(e) => setBio(e.target.value)} disabled={!isOwner} maxLength={500} placeholder="I make videos about…" className="mt-1" />
          </div>
          <div>
            <label className="text-sm font-medium text-slate-700">Accent color</label>
            <div className="mt-1 flex items-center gap-2">
              <input type="color" value={/^#[0-9a-fA-F]{6}$/.test(primaryColor) ? primaryColor : "#0f172a"} onChange={(e) => setPrimaryColor(e.target.value)} disabled={!isOwner} className="h-10 w-14 cursor-pointer rounded border border-slate-300" />
              <Input value={primaryColor} onChange={(e) => setPrimaryColor(e.target.value)} disabled={!isOwner} placeholder="#0f172a" className="font-mono text-xs" />
            </div>
          </div>
          {isOwner ? (
            <Button type="submit" disabled={!dirty || saving}>{saving ? "Saving..." : "Save profile"}</Button>
          ) : (
            <p className="text-sm text-slate-500">Only owners can edit the page.</p>
          )}
        </form>
      </CardContent>
    </Card>
  );
};

const MyPage = () => {
  const accountQuery = useQuery({ queryKey: ["individual", "account"], queryFn: () => getIndividualAccount(), staleTime: 30_000 });
  const account = accountQuery.data;
  const isOwner = account?.role === "owner";

  if (accountQuery.isLoading) {
    return <Skeleton className="mt-4 h-64 w-full" />;
  }
  if (!account || (account.account_kind ?? "merchant") !== "creator") {
    return (
      <div>
        <h2 className="text-2xl font-bold text-slate-900">My Page</h2>
        <p className="mt-1 text-sm text-slate-500">
          Individual pages live here. This account is on the business track —{" "}
          <Link to="/merchant/apps" className="text-blue-600 hover:underline">go to your apps</Link>.
        </p>
      </div>
    );
  }

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">My Page</h2>
          <p className="mt-1 text-sm text-slate-500">Edit how fans see you, manage support buttons, and share.</p>
        </div>
        {account.handle && (
          <a href={`/c/${account.handle}`} target="_blank" rel="noopener noreferrer" className="text-sm text-blue-600 hover:underline">
            Preview public page →
          </a>
        )}
      </div>

      <MyPageProfile account={account} isOwner={!!isOwner} />

      <Card className="mt-4">
        <CardContent className="p-4 sm:p-6">
          <h3 className="text-sm font-bold text-slate-800">Support buttons</h3>
          <p className="mt-1 text-xs text-slate-500">Fixed presets and the custom-amount button fans tap first.</p>
          <SupportPageEditor org={account} isOwner={!!isOwner} track="individual" />
        </CardContent>
      </Card>

      {account.handle && <SharePanel handle={account.handle} displayName={account.display_name || account.name} />}
    </div>
  );
};

export default MyPage;
