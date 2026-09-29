import { Fragment, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { fetchFailures } from "@/lib/analyticsApi";
import { CsvButton, DateRangePicker, presetRange } from "@/pages/analyticsCommon";

const FAILURE_HELP: Record<string, { en: string; sw: string }> = {
  expired: { en: "Buyer never completed payment before the order TTL.", sw: "Mnunuzi hakumaliza malipo kabla ya muda kuisha." },
  cancelled: { en: "Order was cancelled before completion.", sw: "Oda ilighairiwa kabla haijakamilika." },
  provider_declined: { en: "Provider rejected it (often insufficient funds or wrong PIN).", sw: "Mtoa huduma alikataa (mara nyingi salio pungufu au PIN sio sahihi)." },
  timeout: { en: "Provider did not answer in time — usually transient.", sw: "Mtoa huduma hakujibu kwa wakati — kwa kawaida ni la muda." },
  system_error: { en: "Internal/provider error worth a look.", sw: "Hitilafu ya ndani au ya mtoa huduma inayohitaji kuangaliwa." },
  unspecified: { en: "No normalized reason recorded (older rows).", sw: "Hakuna sababu iliyorekodiwa (oda za zamani)." },
};

const AdminAnalyticsFailures = () => {
  const initial = presetRange("30d");
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);
  const [expanded, setExpanded] = useState<string | null>(null);

  const failuresQuery = useQuery({
    queryKey: ["admin", "analytics", "failures", from, to],
    queryFn: () => fetchFailures({ from, to }),
    staleTime: 30_000,
  });
  const items = failuresQuery.data ?? [];

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Failures</h2>
          <p className="mt-1 text-sm text-slate-500">Ranked normalized reasons · live payments only</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <DateRangePicker from={from} to={to} onChange={(f, t) => { setFrom(f); setTo(t); }} />
          <CsvButton report="failures" query={{ from, to }} />
        </div>
      </div>

      <Card className="mt-4"><CardContent className="overflow-x-auto p-0">
        {failuresQuery.isLoading ? (
          <div className="p-4"><Skeleton className="h-40 w-full" /></div>
        ) : failuresQuery.error ? (
          <p className="p-4 text-sm text-red-600">Unable to load failures.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Reason</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>Count</TableHead>
                <TableHead>Merchants</TableHead>
                <TableHead className="text-right">Samples</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((f) => {
                const key = `${f.code}|${f.provider}`;
                const help = FAILURE_HELP[f.code];
                return (
                  <Fragment key={key}>
                    <TableRow key={key}>
                      <TableCell>
                        <Badge variant="outline">{f.code}</Badge>
                        {help && <p className="mt-1 max-w-xs text-xs text-slate-500">{help.en}</p>}
                      </TableCell>
                      <TableCell>{f.provider}</TableCell>
                      <TableCell className="font-bold">{f.count}</TableCell>
                      <TableCell>{f.affected_orgs}</TableCell>
                      <TableCell className="text-right">
                        <Button size="sm" variant="outline" onClick={() => setExpanded(expanded === key ? null : key)}>
                          {expanded === key ? "Hide" : `Samples (${f.sample_order_ids.length})`}
                        </Button>
                      </TableCell>
                    </TableRow>
                    {expanded === key && (
                      <TableRow key={`${key}-samples`}>
                        <TableCell colSpan={5} className="bg-slate-50">
                          <p className="text-xs font-semibold text-slate-600">Sample order ids (newest first)</p>
                          {f.sample_order_ids.length === 0 ? (
                            <p className="mt-1 text-xs text-slate-500">No samples.</p>
                          ) : (
                            <ul className="mt-1 space-y-1">
                              {f.sample_order_ids.map((id) => (
                                <li key={id} className="font-mono text-xs text-slate-700">
                                  {id} — <Link to="/admin/payments" className="font-sans text-blue-600 hover:underline">inspect in Payments →</Link>
                                </li>
                              ))}
                            </ul>
                          )}
                          {help && <p className="mt-2 text-xs text-slate-500">Kiswahili: {help.sw}</p>}
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                );
              })}
              {items.length === 0 && (
                <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No failures in range — clean sheet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        )}
      </CardContent></Card>
    </div>
  );
};

export default AdminAnalyticsFailures;
