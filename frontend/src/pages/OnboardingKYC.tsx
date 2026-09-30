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
import { getKYC, submitCreatorKYC, submitKYC, uploadKYCDocument, uploadKYCSelfie } from "@/lib/signupApi";

const ID_TYPES = [
  { value: "national_id", label: "National ID (NIDA — Tanzanian citizens)" },
  { value: "passport", label: "Passport (non-citizens welcome)" },
  { value: "drivers_license", label: "Driver's license" },
  { value: "voters_id", label: "Voter's ID" },
] as const;

// Client-side 18+ check mirrors the server age-gate so under-18 users get
// the clear message before uploading anything.
function isAdultDob(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const dob = new Date(value + "T00:00:00Z");
  if (Number.isNaN(dob.getTime())) return false;
  const now = new Date();
  const eighteen = new Date(Date.UTC(now.getUTCFullYear() - 18, now.getUTCMonth(), now.getUTCDate()));
  return dob <= eighteen && dob.getUTCFullYear() >= 1900 && dob <= now;
}

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
  const backFileInput = useRef<HTMLInputElement>(null);
  const selfieInput = useRef<HTMLInputElement>(null);

  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000 });
  const kycQuery = useQuery({
    queryKey: ["kyc", orgId],
    queryFn: () => getKYC(orgId),
    staleTime: 15_000,
    retry: false,
  });

  const isOwner = orgQuery.data?.role === "owner";
  const status = orgQuery.data?.kyc_status ?? "pending";
  const isCreator = (orgQuery.data?.account_kind ?? "merchant") === "creator";

  const [businessName, setBusinessName] = useState("");
  const [tin, setTin] = useState("");
  const [fullName, setFullName] = useState("");
  const [idType, setIdType] = useState<string>("national_id");
  const [idNumber, setIdNumber] = useState("");
  const [dob, setDob] = useState("");
  const [docURL, setDocURL] = useState("");
  const [fileName, setFileName] = useState("");
  const [docBackURL, setDocBackURL] = useState("");
  const [backFileName, setBackFileName] = useState("");
  const [selfieURL, setSelfieURL] = useState("");
  const [selfieFileName, setSelfieFileName] = useState("");
  const [uploading, setUploading] = useState(false);
  const [uploadingBack, setUploadingBack] = useState(false);
  const [uploadingSelfie, setUploadingSelfie] = useState(false);
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

  const handleUploadBack = async (file: File | undefined) => {
    if (!file || !orgId) return;
    if (file.size > 5 * 1024 * 1024) {
      toast.error("Document must be under 5MB.");
      return;
    }
    setUploadingBack(true);
    try {
      const result = await uploadKYCDocument(orgId, file);
      setDocBackURL(result.id_document_url);
      setBackFileName(file.name);
      toast.success("Back side uploaded.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to upload document.");
    } finally {
      setUploadingBack(false);
    }
  };

  const handleUploadSelfie = async (file: File | undefined) => {
    if (!file || !orgId) return;
    if (file.size > 5 * 1024 * 1024) {
      toast.error("Selfie must be under 5MB.");
      return;
    }
    if (!/^(image\/jpeg|image\/png|image\/webp)$/.test(file.type)) {
      toast.error("Selfie must be a JPEG, PNG, or WEBP photo.");
      return;
    }
    setUploadingSelfie(true);
    try {
      const result = await uploadKYCSelfie(orgId, file);
      setSelfieURL(result.selfie_url);
      setSelfieFileName(file.name);
      toast.success("Selfie uploaded.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to upload selfie.");
    } finally {
      setUploadingSelfie(false);
    }
  };

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (!orgId) return;
    if (isCreator && !isAdultDob(dob)) {
      toast.error("You must be 18 or older to use LipaGO.");
      return;
    }
    setSubmitting(true);
    try {
      if (isCreator) {
        await submitCreatorKYC(orgId, {
          full_name: fullName.trim(),
          id_type: idType,
          id_number: idNumber.trim(),
          dob,
          id_document_url: docURL,
          id_document_back_url: docBackURL || undefined,
          selfie_url: selfieURL,
        });
      } else {
        await submitKYC(orgId, { business_name: businessName.trim(), tin: tin.trim(), id_document_url: docURL });
      }
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
          <h2 className="text-2xl font-bold text-slate-900">
            {isCreator ? "Verify your identity" : "Verify your organization"}
          </h2>
          <p className="mt-1 flex items-center gap-2 text-sm text-slate-500">
            {isCreator
              ? "Live payouts require ID verification. Sandbox mode works without it."
              : "Live payments require verification. Sandbox mode works without it."}
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
              <p className="font-semibold text-slate-900">{isCreator ? "Identity verified" : "Organization verified"}</p>
              <p className="mt-0.5 text-slate-500">
                {isCreator ? "Live payouts and live support payments are enabled." : "Live API keys and live payments are enabled."}
              </p>
            </div>
          </CardContent>
        </Card>
      ) : (
        <>
          {kycQuery.data && (
            <Card className="mt-4">
              <CardContent className="p-4 text-sm text-slate-600">
                <p className="font-medium text-slate-800">Current submission</p>
                <p className="mt-1">{kycQuery.data.submission.full_name || kycQuery.data.submission.business_name || "—"}</p>
                {kycQuery.data.submission.rejection_reason && (
                  <p className="mt-1 text-rose-600">Rejection reason: {kycQuery.data.submission.rejection_reason}</p>
                )}
              </CardContent>
            </Card>
          )}

          <Card className="mt-4">
            <CardContent className="p-4">
              <form onSubmit={handleSubmit} className="space-y-4">
                {isCreator ? (
                  <>
                    <div>
                      <label className="text-sm font-medium text-slate-700">Full legal name</label>
                      <Input
                        value={fullName}
                        onChange={(e) => setFullName(e.target.value)}
                        required
                        maxLength={200}
                        placeholder="Amina Juma"
                        className="mt-1"
                      />
                      <p className="mt-1 text-xs text-slate-400">Must match your ID document exactly.</p>
                    </div>
                    <div>
                      <label className="text-sm font-medium text-slate-700">Date of birth</label>
                      <Input
                        type="date"
                        value={dob}
                        onChange={(e) => setDob(e.target.value)}
                        required
                        max={new Date().toISOString().slice(0, 10)}
                        className="mt-1"
                      />
                      <p className="mt-1 text-xs text-slate-400">You must be 18 or older to use LipaGO.</p>
                      {dob && !isAdultDob(dob) && (
                        <p className="mt-1 text-xs text-rose-600">You must be 18 or older to use LipaGO.</p>
                      )}
                    </div>
                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                      <div>
                        <label className="text-sm font-medium text-slate-700">ID type</label>
                        <select
                          className="mt-1 h-10 w-full rounded-md border border-slate-300 px-2 text-sm"
                          value={idType}
                          onChange={(e) => setIdType(e.target.value)}
                        >
                          {ID_TYPES.map((t) => (
                            <option key={t.value} value={t.value}>{t.label}</option>
                          ))}
                        </select>
                      </div>
                      <div>
                        <label className="text-sm font-medium text-slate-700">ID number</label>
                        <Input
                          value={idNumber}
                          onChange={(e) => setIdNumber(e.target.value)}
                          required
                          maxLength={60}
                          placeholder="19900101-00001-00001-00"
                          className="mt-1"
                        />
                        <p className="mt-1 text-xs text-slate-400">NIDA number for Tanzanian citizens, passport number otherwise.</p>
                      </div>
                    </div>
                  </>
                ) : (
                  <>
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
                  </>
                )}
                <div>
                  <label className="text-sm font-medium text-slate-700">
                    {isCreator ? "ID document — front (JPEG, PNG, WEBP, or PDF — max 5MB)" : "ID document (JPEG, PNG, WEBP, or PDF — max 5MB)"}
                  </label>
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
                {isCreator && (
                  <>
                    <div>
                      <label className="text-sm font-medium text-slate-700">ID document — back <span className="font-normal text-slate-400">(optional, if your ID has two sides)</span></label>
                      <input
                        ref={backFileInput}
                        type="file"
                        accept=".jpg,.jpeg,.png,.webp,.pdf,image/jpeg,image/png,image/webp,application/pdf"
                        className="hidden"
                        onChange={(e) => handleUploadBack(e.target.files?.[0])}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        className="mt-1 w-full"
                        disabled={uploadingBack}
                        onClick={() => backFileInput.current?.click()}
                      >
                        <Upload className="mr-2 h-4 w-4" />
                        {uploadingBack ? "Uploading..." : backFileName || "Choose back side"}
                      </Button>
                      {docBackURL && <p className="mt-1 text-xs text-emerald-600">Uploaded ✓</p>}
                    </div>
                    <div>
                      <label className="text-sm font-medium text-slate-700">Selfie photo (JPEG, PNG, or WEBP — max 5MB)</label>
                      <input
                        ref={selfieInput}
                        type="file"
                        accept=".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp"
                        className="hidden"
                        onChange={(e) => handleUploadSelfie(e.target.files?.[0])}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        className="mt-1 w-full"
                        disabled={uploadingSelfie}
                        onClick={() => selfieInput.current?.click()}
                      >
                        <Upload className="mr-2 h-4 w-4" />
                        {uploadingSelfie ? "Uploading..." : selfieFileName || "Take or choose selfie"}
                      </Button>
                      {selfieURL && <p className="mt-1 text-xs text-emerald-600">Uploaded ✓</p>}
                      <p className="mt-1 text-xs text-slate-400">A clear front-facing photo. Automated liveness checks are future scope — a reviewer compares it to your ID.</p>
                    </div>
                  </>
                )}
                <Button
                  type="submit"
                  className="w-full"
                  disabled={submitting || !docURL || (isCreator && (!selfieURL || (dob !== "" && !isAdultDob(dob))))}
                >
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
