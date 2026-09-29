import { useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listMyApps } from "@/lib/merchantApi";
import { fetchMerchantFailures } from "@/lib/merchantAnalyticsApi";
import { AppFilter, AnalyticsSubNav, EnvToggle } from "@/pages/merchantAnalyticsCommon";
import { DateRangePicker, presetRange } from "@/pages/analyticsCommon";

const FAILURE_HELP: Record<string, { en: string; sw: string; fix: string }> = {
  expired: {
    en: "Buyer never completed payment before the order TTL.",
    sw: "Mnunuzi hakumaliza malipo kabla ya muda kuisha.",
    fix: "Send payment reminders sooner; consider a longer TTL for slow payers.",
  },
  cancelled: {
    en: "Order was cancelled before completion.",
    sw: "Oda ilighairiwa kabla haijakamilika.",
    fix: "Check your checkout flow for accidental cancellations.",
  },
  provider_declined: {
    en: "Provider rejected it (often insufficient funds or wrong PIN).",
    sw: "Mtoa huduma alikataa (mara nyingi salio pungufu au PIN sio sahihi).",
    fix: "Tell customers to confirm balance and PIN, then retry.",
  },
  timeout: {
    en: "Provider did not answer in time — usually transient.",
    sw: "Mtoa huduma hakujibu kwa wakati — kwa kawaida ni la muda.",
    fix: "Safe to retry; check provider status if it persists.",
  },
  system_error: {
    en: "Internal/provider error worth a look.",
    sw: "Hitilafu ya ndani au ya mtoa huduma inayohitaji kuangaliwa.",
    fix: "Contact support with a sample order id.",
  },
  unspecified: {
    en: "No normalized reason recorded (older rows).",
    sw: "Hakuna sababu iliyorekodiwa (oda za zamani).",
    fix: "Newer orders always carry a reason.",
  },
};

const MerchantAnalyticsFailures = () => {
  const { orgId = "" } = useParams();
  const initial = presetRange("30d");
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);
  const [env, setEnv] = useState<"live" | "sandbox">("live");
  const [appId, setAppId] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const failuresQuery = useQuery({
    queryKey: ["merchant", "analytics", "failures", orgId, from, to, env, appId],
    queryFn: () => fetchMerchantFailures(orgId, { from, to, environment: env, app_id: appId || undefined }),
    staleTime: 30_000,
  });
  const items = failuresQuery.data ?? [];

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Failures</h2>
          <p className="mt-1 text-sm text-slate-500">Why your payments fail — with fixes, in English na Kiswahili</p>
          <AnalyticsSubNav orgId={orgId} />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EnvToggle env={env} onChange={setEnv} />
          <AppFilter apps={appsQuery.data ?? []} value={appId} onChange={setAppId} />
          <DateRangePicker from={from} to={to} onChange={(f, t) => { setFrom(f); setTo(t); }} />
        </div>
      </div>

      <Card className="mt-4"><CardContent className="overflow-x-auto p-0">
        {failuresQuery.isLoading ? (
          <div className="p-4"><Skeleton className="h-40 w-full" /></div>
        ) : failuresQuery.error ? (
          <p className="p-4 text-sm text-red-600">Unable to load failures.</p>
        ) : (
          <Table>
            <TableHeader><TableRow>
              <TableHead>Reason</TableHead><TableHead>Provider</TableHead>
              <TableHead>Count</TableHead><TableHead>What to do</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {items.map((f) => {
                const help = FAILURE_HELP[f.code] ?? FAILURE_HELP.unspecified;
                return (
                  <TableRow key={`${f.code}|${f.provider}`}>
                    <TableCell>
                      <Badge variant="outline">{f.code}</Badge>
                      <p className="mt-1 max-w-xs text-xs text-slate-500">{help.en}</p>
                      <p className="max-w-xs text-xs text-slate-400">Kiswahili: {help.sw}</p>
                    </TableCell>
                    <TableCell>{f.provider}</TableCell>
                    <TableCell className="font-bold">{f.count}</TableCell>
                    <TableCell className="max-w-xs text-xs text-slate-600">{help.fix}</TableCell>
                  </TableRow>
                );
              })}
              {items.length === 0 && (
                <TableRow><TableCell colSpan={4} className="text-center text-slate-500">No failures in range — clean sheet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        )}
      </CardContent></Card>
    </div>
  );
};

export default MerchantAnalyticsFailures;
