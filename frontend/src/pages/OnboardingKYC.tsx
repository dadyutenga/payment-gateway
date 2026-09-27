import { FormEvent, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileCheck2, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import { getOrg } from "@/lib/orgApi";
import { getKYC, submitKYC, uploadKYCDocument } from "@/lib/signupApi";

const STATUS_BADGE: Record<string, string> = {
  pending: "bg-slate-100 text-slate-700",
  submitted: "bg-amber-100 text-amber-800",
  verified: "bg-emerald-100 text-emerald-800",
  rejected: "bg-rose-100 text-rose-800",
};

const OnboardingKYC = () => {
  const { orgId = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const fileInput = useRef<HTMLInputElement>(null);

  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000 });
  const kycQuery = useQuery({
    queryKey: ["kyc", orgId],
    queryFn: () => getKYC(orgId),
    staleTime: 15_000,
    retry: false,
  });

  const isOwner = orgQuery.data?.role === "owner";
  const status = orgQuery.data?.kyc_status ?? "pending";

  const [businessName, setBusinessName] = useState("");
  const [tin, setTin] = useState("");
  const [docURL, setDocURL] = useState("");
  const [fileName, setFileName] = useState("");
  const [uploading, setUploading] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const handleUpload = async (file: File | undefined) => {
    if (!file || !orgId) return;
    if (file.size > 5 * 1024 * 1024) {
      toast.error("Document must be under 5MB.");
      return;
    }
    setUploading(true);
    try {
      const result = await uploadKYCDocument(orgId, file);
      setDocURL(result.id_document_url);
      setFileName(file.name);
      toast.success("Document uploaded.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to upload document.");
    } finally {
      setUploading(false);
    }
  };

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (!orgId) return;
    setSubmitting(true);
    try {
      await submitKYC(orgId, { business_name: businessName.trim(), tin: tin.trim(), id_document_url: docURL });
      toast.success("Verification submitted — an admin will review it.");
      queryClient.invalidateQueries({ queryKey: ["orgs", orgId] });
      queryClient.invalidateQueries({ queryKey: ["kyc", orgId] });
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
      navigate(`/org/${orgId}/settings`, { replace: true });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to submit verification.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="mx-auto max-w-xl">
      <div className="flex items-center justify-between gap-2">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Verify your organization</h2>
          <p className="mt-1 flex items-center gap-2 text-sm text-slate-500">
            Live payments require verification. Sandbox mode works without it.
            <Badge className={STATUS_BADGE[status] ?? ""} variant="secondary">{status}</Badge>
          </p>
        </div>
        <Link to={`/org/${orgId}/settings`} className="text-sm text-blue-600 hover:underline">Back</Link>
      </div>

      {orgQuery.isLoading ? (
        <Skeleton className="mt-4 h-64 w-full" />
      ) : !isOwner ? (
        <Card className="mt-4">
          <CardContent className="p-4 text-sm text-slate-500">
            Only owners can submit verification documents.
          </CardContent>
        </Card>
      ) : status === "verified" ? (
        <Card className="mt-4">
          <CardContent className="flex items-start gap-3 p-4">
            <FileCheck2 className="mt-0.5 h-5 w-5 shrink-0 text-emerald-500" />
            <div className="text-sm">
              <p className="font-semibold text-slate-900">Organization verified</p>
              <p className="mt-0.5 text-slate-500">Live API keys and live payments are enabled.</p>
            </div>
          </CardContent>
        </Card>
      ) : (
        <>
          {kycQuery.data && (
            <Card className="mt-4">
              <CardContent className="p-4 text-sm text-slate-600">
                <p className="font-medium text-slate-800">Current submission</p>
                <p className="mt-1">{kycQuery.data.submission.business_name || "—"}</p>
                {kycQuery.data.submission.rejection_reason && (
                  <p className="mt-1 text-rose-600">Rejection reason: {kycQuery.data.submission.rejection_reason}</p>
                )}
              </CardContent>
            </Card>
          )}

          <Card className="mt-4">
            <CardContent className="p-4">
              <form onSubmit={handleSubmit} className="space-y-4">
                <div>
                  <label className="text-sm font-medium text-slate-700">Registered business name</label>
                  <Input
                    value={businessName}
                    onChange={(e) => setBusinessName(e.target.value)}
                    required
                    maxLength={200}
                    placeholder="Acme Limited"
                    className="mt-1"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium text-slate-700">TIN (9–20 digits)</label>
                  <Input
                    value={tin}
                    onChange={(e) => setTin(e.target.value.replace(/[^\d\s-]/g, ""))}
                    required
                    inputMode="numeric"
                    placeholder="123456789"
                    className="mt-1"
                  />
                </div>
                <div>
                  <label className="text-sm font-medium text-slate-700">ID document (JPEG, PNG, WEBP, or PDF — max 5MB)</label>
                  <input
                    ref={fileInput}
                    type="file"
                    accept=".jpg,.jpeg,.png,.webp,.pdf,image/jpeg,image/png,image/webp,application/pdf"
                    className="hidden"
                    onChange={(e) => handleUpload(e.target.files?.[0])}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    className="mt-1 w-full"
                    disabled={uploading}
                    onClick={() => fileInput.current?.click()}
                  >
                    <Upload className="mr-2 h-4 w-4" />
                    {uploading ? "Uploading..." : fileName || "Choose document"}
                  </Button>
                  {docURL && <p className="mt-1 text-xs text-emerald-600">Uploaded ✓</p>}
                </div>
                <Button type="submit" className="w-full" disabled={submitting || !docURL}>
                  {submitting ? "Submitting..." : "Submit for review"}
                </Button>
              </form>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
};

export default OnboardingKYC;
