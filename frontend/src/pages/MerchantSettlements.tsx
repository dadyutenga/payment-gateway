import { useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { toast } from "@/components/ui/sonner";
import { listMyApps } from "@/lib/merchantApi";
import { getOrg } from "@/lib/orgApi";
import {
  downloadSettlement, fetchSettlement, type SettlementData,
} from "@/lib/merchantAnalyticsApi";
import { AppFilter, AnalyticsSubNav, EnvToggle } from "@/pages/merchantAnalyticsCommon";
import { useFilterParams } from "@/pages/analyticsCommon";

function eatToday(): string {
  const fmt = new Intl.DateTimeFormat("en-CA", { timeZone: "Africa/Dar_es_Salaam", year: "numeric", month: "2-digit", day: "2-digit" });
  return fmt.format(new Date());
}

const PERIODS = [
  { label: "This month", get: () => { const t = eatToday(); return { from: t.slice(0, 8) + "01", to: t }; } },
  {
    label: "Last month", get: () => {
      const t = eatToday();
      const first = new Date(`${t.slice(0, 8)}01T12:00:00Z`);
      first.setUTCMonth(first.getUTCMonth() - 1);
      const f = first.toISOString().slice(0, 10);
      const last = new Date(`${t.slice(0, 8)}01T12:00:00Z`);
      last.setUTCDate(last.getUTCDate() - 1);
      return { from: f.slice(0, 8) + "01", to: last.toISOString().slice(0, 10) };
    },
  },
  {
    label: "Last 90 days", get: () => {
      const t = eatToday();
      const d = new Date(`${t}T12:00:00Z`);
      d.setUTCDate(d.getUTCDate() - 89);
      return { from: d.toISOString().slice(0, 10), to: t };
    },
  },
];

const MerchantSettlements = () => {
  const { orgId = "" } = useParams();
  const urlFilters = useFilterParams();
  const initial = { from: urlFilters.from, to: urlFilters.to };
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);
  const [env, setEnv] = useState<"live" | "sandbox">("live");
  const [appId, setAppId] = useState("");
  const [currency, setCurrency] = useState("TZS");
  const [applied, setApplied] = useState<{ from: string; to: string } | null>(null);
  const [busy, setBusy] = useState<"csv" | "pdf" | null>(null);

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const roleQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000 });
  const canExport = roleQuery.data ? roleQuery.data.role !== "viewer" : false;

  const settlementQuery = useQuery({
    queryKey: ["merchant", "settlements", orgId, applied, env, appId],
    queryFn: () => fetchSettlement(orgId, {
      from: applied!.from, to: applied!.to, environment: env,
      app_id: appId || undefined,
    }),
    enabled: applied !== null,
    staleTime: 30_000,
  });
  const statement: SettlementData | undefined = settlementQuery.data;

  const generate = (f: string, t: string) => {
    setFrom(f);
    setTo(t);
    urlFilters.setRange(f, t);
    setApplied({ from: f, to: t });
  };

  const download = async (format: "csv" | "pdf") => {
    if (!applied) return;
    setBusy(format);
    try {
      await downloadSettlement(orgId, format, {
        from: applied.from, to: applied.to, environment: env,
        app_id: appId || undefined, currency: format === "pdf" ? currency : undefined,
      });
      toast.success(`${format.toUpperCase()} downloaded.`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to build statement.");
    } finally {
      setBusy(null);
    }
  };

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Settlements</h2>
          <p className="mt-1 text-sm text-slate-500">Ledger-based statements — totals always equal the ledger</p>
          <AnalyticsSubNav orgId={orgId} />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EnvToggle env={env} onChange={setEnv} />
          <AppFilter apps={appsQuery.data ?? []} value={appId} onChange={setAppId} />
        </div>
      </div>

      <Card className="mt-4"><CardContent className="p-4">
        <div className="flex flex-wrap items-center gap-1.5">
          {PERIODS.map((p) => (
            <Button key={p.label} size="sm" variant="outline" onClick={() => { const r = p.get(); generate(r.from, r.to); }}>
              {p.label}
            </Button>
          ))}
          <Input type="date" aria-label="From date" value={from} onChange={(e) => setFrom(e.target.value)} className="h-8 w-36" />
          <span className="text-xs text-slate-400">→</span>
          <Input type="date" aria-label="To date" value={to} onChange={(e) => setTo(e.target.value)} className="h-8 w-36" />
          <Button size="sm" onClick={() => generate(from, to)}>Generate</Button>
        </div>
        {canExport && applied && (
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Input
              aria-label="Statement currency for PDF"
              value={currency}
              onChange={(e) => setCurrency(e.target.value.toUpperCase())}
              maxLength={8}
              placeholder="TZS"
              className="h-8 w-24"
            />
            <Button size="sm" variant="outline" disabled={busy !== null} onClick={() => download("csv")}>
              <Download className="h-3.5 w-3.5 mr-1" /> {busy === "csv" ? "Building..." : "CSV"}
            </Button>
            <Button size="sm" variant="outline" disabled={busy !== null} onClick={() => download("pdf")}>
              <Download className="h-3.5 w-3.5 mr-1" /> {busy === "pdf" ? "Building..." : "PDF"}
            </Button>
          </div>
        )}
      </CardContent></Card>

      {settlementQuery.isLoading && <Card className="mt-4"><CardContent className="p-4"><Skeleton className="h-40 w-full" /></CardContent></Card>}
      {settlementQuery.error && <p className="mt-4 text-sm text-red-600">Unable to build statement.</p>}
      {statement && (
        <div className="mt-4 space-y-4">
          <p className="text-xs text-slate-500">
            {statement.statement_id} · {statement.org_name} · generated {statement.generated_at ? new Date(statement.generated_at).toLocaleString() : "—"}
            {statement.truncated && " · entry list truncated — narrow the range or use CSV"}
          </p>
          {statement.blocks.map((b) => (
            <Card key={b.currency}><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">Currency: {b.currency}</h3>
              <div className="mt-2 grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
                <div><p className="text-[11px] uppercase tracking-wide text-slate-400">Gross</p><p className="font-bold">{b.gross}</p></div>
                <div><p className="text-[11px] uppercase tracking-wide text-slate-400">Fees</p><p className="font-bold">{b.fees}</p></div>
                <div><p className="text-[11px] uppercase tracking-wide text-slate-400">Refunds</p><p className="font-bold">{b.refunds}</p></div>
                <div><p className="text-[11px] uppercase tracking-wide text-slate-400">Net settled</p><p className="font-extrabold text-emerald-700">{b.net_settled}</p></div>
                <div><p className="text-[11px] uppercase tracking-wide text-slate-400">Opening</p><p>{b.opening_balance}</p></div>
                <div><p className="text-[11px] uppercase tracking-wide text-slate-400">Closing</p><p>{b.closing_balance}</p></div>
              </div>
              {b.by_provider.length > 0 && (
                <Table>
                  <TableHeader><TableRow><TableHead>Provider</TableHead><TableHead>Gross</TableHead><TableHead>Fees</TableHead><TableHead>Count</TableHead></TableRow></TableHeader>
                  <TableBody>
                    {b.by_provider.map((p) => (
                      <TableRow key={p.provider}>
                        <TableCell>{p.provider}</TableCell>
                        <TableCell className="text-xs">{p.gross}</TableCell>
                        <TableCell className="text-xs">{p.fees}</TableCell>
                        <TableCell>{p.count}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
              <h4 className="mt-3 text-xs font-bold text-slate-600">Itemized entries ({b.entries.length})</h4>
              <Table>
                <TableHeader><TableRow>
                  <TableHead>Date</TableHead><TableHead>Type</TableHead><TableHead>Amount</TableHead><TableHead>Description</TableHead>
                </TableRow></TableHeader>
                <TableBody>
                  {b.entries.slice(0, 50).map((e) => (
                    <TableRow key={e.id}>
                      <TableCell className="text-xs">{e.created_at ? new Date(e.created_at).toLocaleString() : "—"}</TableCell>
                      <TableCell><Badge variant="outline">{e.entry_type.replace(/_/g, " ")}</Badge></TableCell>
                      <TableCell className={e.direction === "credit" ? "text-emerald-700" : "text-rose-700"}>
                        {e.direction === "credit" ? "+" : "-"}{e.amount}
                      </TableCell>
                      <TableCell className="max-w-xs truncate text-xs text-slate-500">{e.description || "—"}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {b.entries.length > 50 && (
                <p className="mt-1 text-xs text-slate-400">Showing 50 of {b.entries.length} — download CSV for the full list.</p>
              )}
            </CardContent></Card>
          ))}
        </div>
      )}
    </div>
  );
};

export default MerchantSettlements;
