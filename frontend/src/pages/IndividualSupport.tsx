import { FormEvent, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, Copy, HeartHandshake } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import { createSupportOrder, getSupportPage, type SupportOrder } from "@/lib/orgApi";

type Lang = "en" | "sw";

// Bilingual copy for the public support page (EN/SW toggle). Kept as a
// local dictionary — the same pattern Block 3's checkout should reuse so
// both surfaces share one language switch.
const STR: Record<string, { en: string; sw: string }> = {
  supportAmount: { en: "Support amount (TZS)", sw: "Kiasi cha mchango (TZS)" },
  customAmount: { en: "Custom amount", sw: "Kiasi unachotaka" },
  yourName: { en: "Your name", sw: "Jina lako" },
  phone: { en: "Mobile-money phone", sw: "Namba ya simu (pesa mfukoni)" },
  email: { en: "Email", sw: "Barua pepe" },
  provider: { en: "Pay with", sw: "Lipa kwa" },
  message: { en: "Message for the individual (optional)", sw: "Ujumbe kwa mtu binafsi (hiari)" },
  messageHint: { en: "Shown privately to the individual in their dashboard — never public.", sw: "Huonekana na mtu huyo tu kwenye dashibodi yake — si hadharani." },
  pay: { en: "Send support", sw: "Tuma mchango" },
  sending: { en: "Sending…", sw: "Inatuma…" },
  minMax: { en: "Between", sw: "Kati ya" },
  and: { en: "and", sw: "na" },
  sandboxNote: { en: "Sandbox mode — no real money moves until verification completes.", sw: "Hali ya majaribio — hakuna pesa halisi hadi uthibitisho ukamilike." },
  notEnabled: { en: "is not accepting support just yet — check back soon.", sw: "hapokei michango kwa sasa — rudi tena baadaye." },
  created: { en: "Support order created!", sw: "Oda ya mchango imeundwa!" },
  complete: { en: "Complete the payment on your phone to finish. Order status:", sw: "Kamilisha malipo kwenye simu yako. Hali ya oda:" },
  supportAnother: { en: "Send another", sw: "Tuma nyingine" },
  copyLink: { en: "Copy support link", sw: "Nakili kiungo" },
  copied: { en: "Support link copied.", sw: "Kiungo kimenakiliwa." },
  copyFailed: { en: "Unable to copy the link.", sw: "Imeshindwa kunakili kiungo." },
  notFound: { en: "Individual not found", sw: "Mtu huyo hajapatikana" },
  checkHandle: { en: "Check the handle and try again.", sw: "Angalia jina na ujaribu tena." },
  back: { en: "Back to LipaGO", sw: "Rudi LipaGO" },
  failed: { en: "Unable to send support.", sw: "Imeshindwa kutuma mchango." },
  powered: { en: "Powered by", sw: "Inaendeshwa na" },
};

const CATEGORY_LABEL: Record<string, { en: string; sw: string }> = {
  content_creator: { en: "Content creation", sw: "Uundaji wa maudhui" },
  musician_artist: { en: "Musician / artist", sw: "Mwanamuziki / msanii" },
  freelancer_consultant: { en: "Freelancer / consultant", sw: "Mfanyakazi huru / mshauri" },
  coach_educator: { en: "Coach / educator", sw: "Kocha / mwalimu" },
  nonprofit_cause: { en: "Nonprofit / cause", sw: "Shirika / sababu" },
  other: { en: "Individual", sw: "Mtu binafsi" },
};

function langOf(): Lang {
  try {
    return localStorage.getItem("lipago_support_lang") === "sw" ? "sw" : "en";
  } catch {
    return "en";
  }
}

// Public support page for individual accounts (no auth). Amount buttons
// and the open-amount box post to the single order path
// (POST /api/v1/c/:handle/support); no second money-moving code path.
const IndividualSupport = () => {
  const { handle = "" } = useParams();
  const [lang, setLang] = useState<Lang>(langOf);
  const t = (key: string) => STR[key]?.[lang] ?? key;

  const [linkId, setLinkId] = useState("");
  const [amount, setAmount] = useState("");
  const [buyerName, setBuyerName] = useState("");
  const [buyerPhone, setBuyerPhone] = useState("");
  const [buyerEmail, setBuyerEmail] = useState("");
  const [provider, setProvider] = useState("");
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  const [done, setDone] = useState<SupportOrder | null>(null);

  const pageQuery = useQuery({
    queryKey: ["support-page", handle.toLowerCase()],
    queryFn: () => getSupportPage(handle),
    staleTime: 30_000,
    retry: false,
  });
  const page = pageQuery.data;
  const accent = /^#[0-9a-fA-F]{6}$/.test(page?.primary_color ?? "") ? page!.primary_color! : "#0f172a";
  const logoSrc = useMemo(() => {
    const loc = (page?.logo_url ?? "").trim();
    return /^https?:\/\//i.test(loc) ? loc : "";
  }, [page?.logo_url]);

  const activeLinks = page?.links ?? [];
  const selectedLink = activeLinks.find((l) => l.id === linkId);
  const fixedSelected = selectedLink?.amount_mode === "fixed";
  const effectiveProvider = provider || page?.providers[0] || "";

  const switchLang = (next: Lang) => {
    setLang(next);
    try {
      localStorage.setItem("lipago_support_lang", next);
    } catch {
      /* ignore */
    }
  };

  const copyLink = () => {
    navigator.clipboard
      .writeText(window.location.href)
      .then(() => toast.success(t("copied")))
      .catch(() => toast.error(t("copyFailed")));
  };

  const handlePay = async (event: FormEvent) => {
    event.preventDefault();
    if (!page?.enabled || sending) return;
    setSending(true);
    try {
      const order = await createSupportOrder(handle, {
        link_id: linkId || undefined,
        amount: fixedSelected && selectedLink?.amount ? selectedLink.amount : amount,
        provider: effectiveProvider,
        buyer_name: buyerName.trim(),
        buyer_email: buyerEmail.trim(),
        buyer_phone: buyerPhone.trim(),
        supporter_message: message.trim() || undefined,
      });
      setDone(order);
      toast.success(t("created"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("failed"));
    } finally {
      setSending(false);
    }
  };

  const reset = () => {
    setDone(null);
    setAmount("");
    setLinkId("");
    setMessage("");
  };

  return (
    <div className="min-h-screen bg-slate-50">
      <div className="h-2 w-full" style={{ backgroundColor: accent }} />
      <div className="mx-auto max-w-md px-4 py-10">
        <div className="mb-4 flex justify-center gap-1 rounded-full bg-white p-1 shadow-sm">
          {(["en", "sw"] as Lang[]).map((l) => (
            <button
              key={l}
              type="button"
              onClick={() => switchLang(l)}
              aria-pressed={lang === l}
              className={`rounded-full px-4 py-1 text-xs font-bold uppercase ${lang === l ? "bg-slate-900 text-white" : "text-slate-500"}`}
            >
              {l === "en" ? "English" : "Kiswahili"}
            </button>
          ))}
        </div>

        {pageQuery.isLoading ? (
          <Skeleton className="h-64 w-full" />
        ) : pageQuery.error || !page ? (
          <Card>
            <CardContent className="p-6 text-center">
              <HeartHandshake className="mx-auto h-8 w-8 text-slate-300" />
              <p className="mt-3 font-semibold text-slate-900">{t("notFound")}</p>
              <p className="mt-1 text-sm text-slate-500">{t("checkHandle")}</p>
              <Button asChild variant="outline" className="mt-4">
                <Link to="/">{t("back")}</Link>
              </Button>
            </CardContent>
          </Card>
        ) : (
          <Card>
            <CardContent className="p-6 text-center">
              {logoSrc ? (
                <img src={logoSrc} alt="" className="mx-auto h-20 w-20 rounded-full object-cover" />
              ) : (
                <span
                  className="mx-auto flex h-20 w-20 items-center justify-center rounded-full text-3xl font-bold text-white"
                  style={{ backgroundColor: accent }}
                >
                  {(page.display_name || "L").slice(0, 1)}
                </span>
              )}
              <h1 className="mt-4 text-2xl font-bold text-slate-900">{page.display_name}</h1>
              <p className="text-sm text-slate-400">@{page.handle}</p>
              {page.category && CATEGORY_LABEL[page.category] && (
                <div className="mt-2"><Badge variant="outline">{CATEGORY_LABEL[page.category][lang]}</Badge></div>
              )}
              {page.bio && <p className="mt-3 text-sm text-slate-600">{page.bio}</p>}

              {!page.enabled ? (
                <p className="mt-6 rounded-lg bg-slate-100 px-3 py-3 text-sm text-slate-500">
                  <strong>{page.display_name}</strong> {t("notEnabled")}
                </p>
              ) : done ? (
                <div className="mt-6 rounded-lg border border-emerald-200 bg-emerald-50 p-4">
                  <CheckCircle2 className="mx-auto h-8 w-8 text-emerald-500" />
                  <p className="mt-2 font-semibold text-slate-900">{t("created")}</p>
                  <p className="mt-1 text-sm text-slate-600">
                    {t("complete")} <Badge variant="secondary">{done.status}</Badge>
                  </p>
                  <p className="mt-1 font-mono text-xs text-slate-400">
                    {done.amount} {done.currency} · {done.provider}
                  </p>
                  <Button variant="outline" size="sm" className="mt-3" onClick={reset}>{t("supportAnother")}</Button>
                </div>
              ) : (
                <form onSubmit={handlePay} className="mt-6 space-y-3 text-left">
                  {activeLinks.length > 0 && (
                    <div className="grid grid-cols-1 gap-2">
                      {activeLinks.map((l) => (
                        <button
                          key={l.id}
                          type="button"
                          onClick={() => {
                            setLinkId(l.id);
                            if (l.amount_mode === "fixed" && l.amount) setAmount(l.amount);
                          }}
                          aria-pressed={linkId === l.id}
                          className={`rounded-lg border px-3 py-2.5 text-sm font-medium transition-colors ${
                            linkId === l.id ? "border-slate-900 bg-slate-900 text-white" : "border-slate-200 hover:border-slate-400"
                          }`}
                        >
                          {l.label}
                          {l.amount_mode === "fixed" && l.amount && <span className="ml-2 opacity-70">{l.amount} TZS</span>}
                        </button>
                      ))}
                    </div>
                  )}

                  <div>
                    <label className="text-sm font-medium text-slate-700">
                      {selectedLink && fixedSelected ? selectedLink.label : t("supportAmount")}
                    </label>
                    <Input
                      value={fixedSelected && selectedLink?.amount ? selectedLink.amount : amount}
                      onChange={(e) => {
                        setAmount(e.target.value.replace(/[^\d]/g, ""));
                        if (fixedSelected) setLinkId("");
                      }}
                      readOnly={fixedSelected}
                      inputMode="numeric"
                      required
                      placeholder="5000"
                      className="mt-1 bg-white text-center text-lg font-bold"
                    />
                    <p className="mt-1 text-xs text-slate-400">
                      {t("minMax")} {page.min_amount || "500"} {t("and")} {page.max_amount || "…"} TZS.
                      {page.environment === "sandbox" && <> {t("sandboxNote")}</>}
                    </p>
                  </div>

                  <div>
                    <label className="text-sm font-medium text-slate-700">{t("yourName")}</label>
                    <Input value={buyerName} onChange={(e) => setBuyerName(e.target.value)} required maxLength={120} className="mt-1" />
                  </div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div>
                      <label className="text-sm font-medium text-slate-700">{t("phone")}</label>
                      <Input value={buyerPhone} onChange={(e) => setBuyerPhone(e.target.value)} required maxLength={32} inputMode="tel" className="mt-1" />
                    </div>
                    <div>
                      <label className="text-sm font-medium text-slate-700">{t("email")}</label>
                      <Input type="email" value={buyerEmail} onChange={(e) => setBuyerEmail(e.target.value)} required maxLength={255} className="mt-1" />
                    </div>
                  </div>

                  {(page.providers?.length ?? 0) > 1 && (
                    <div>
                      <label className="text-sm font-medium text-slate-700">{t("provider")}</label>
                      <select
                        value={effectiveProvider}
                        onChange={(e) => setProvider(e.target.value)}
                        className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-2 text-sm"
                        required
                      >
                        {page.providers.map((p) => (
                          <option key={p} value={p}>{p}</option>
                        ))}
                      </select>
                    </div>
                  )}

                  <div>
                    <label className="text-sm font-medium text-slate-700">{t("message")}</label>
                    <textarea
                      value={message}
                      onChange={(e) => setMessage(e.target.value)}
                      maxLength={280}
                      rows={2}
                      className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                    />
                    <p className="mt-1 flex justify-between text-xs text-slate-400">
                      <span>{t("messageHint")}</span>
                      <span>{message.length}/280</span>
                    </p>
                  </div>

                  <Button type="submit" className="w-full" style={{ backgroundColor: accent }} disabled={sending || !effectiveProvider}>
                    <HeartHandshake className="mr-2 h-4 w-4" /> {sending ? t("sending") : `${t("pay")} · ${page.display_name.split(" ")[0]}`}
                  </Button>
                </form>
              )}

              <Button variant="outline" size="sm" className="mt-4" onClick={copyLink}>
                <Copy className="mr-1 h-3.5 w-3.5" /> {t("copyLink")}
              </Button>
            </CardContent>
          </Card>
        )}
        <p className="mt-6 text-center text-xs text-slate-400">
          {t("powered")} <Link to="/" className="font-medium text-slate-500 hover:underline">LipaGO</Link>
        </p>
      </div>
    </div>
  );
};

export default IndividualSupport;
