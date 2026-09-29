import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { listMyApps } from "@/lib/merchantApi";
import { fetchMerchantPeakHours } from "@/lib/merchantAnalyticsApi";
import { AppFilter, AnalyticsSubNav, EnvToggle } from "@/pages/merchantAnalyticsCommon";
import { DateRangePicker, useFilterParams } from "@/pages/analyticsCommon";

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const MerchantAnalyticsPeakHours = () => {
  const { orgId = "" } = useParams();
  const { from, to, env, setRange, setEnv } = useFilterParams();
  const [appId, setAppId] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const peakQuery = useQuery({
    queryKey: ["merchant", "analytics", "peak", orgId, from, to, env, appId],
    queryFn: () => fetchMerchantPeakHours(orgId, { from, to, environment: env, app_id: appId || undefined }),
    staleTime: 30_000,
  });
  const cells = peakQuery.data?.cells ?? [];

  const maxPaid = useMemo(() => cells.reduce((m, c) => Math.max(m, c.paid_count), 0), [cells]);
  const byDowHour = useMemo(() => {
    const map = new Map<string, number>();
    cells.forEach((c) => map.set(`${c.dow}-${c.hour}`, c.paid_count));
    return map;
  }, [cells]);

  const intensity = (paid: number) => {
    if (maxPaid === 0 || paid === 0) return "bg-slate-100";
    const r = paid / maxPaid;
    if (r > 0.66) return "bg-emerald-600";
    if (r > 0.33) return "bg-emerald-300";
    return "bg-emerald-100";
  };

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Peak hours</h2>
          <p className="mt-1 text-sm text-slate-500">Weekday × hour heatmap (paid orders, EAT)</p>
          <AnalyticsSubNav orgId={orgId} />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EnvToggle env={env} onChange={setEnv} />
          <AppFilter apps={appsQuery.data ?? []} value={appId} onChange={setAppId} />
          <DateRangePicker from={from} to={to} onChange={setRange} />
        </div>
      </div>

      {peakQuery.isLoading ? (
        <Card className="mt-6"><CardContent className="p-4"><Skeleton className="h-48 w-full" /></CardContent></Card>
      ) : peakQuery.error ? (
        <p className="mt-6 text-sm text-red-600">Unable to load peak hours.</p>
      ) : (
        <Card className="mt-6"><CardContent className="p-4">
          {(peakQuery.data?.best_label || peakQuery.data?.worst_label) && (
            <div className="mb-4 space-y-1 text-sm text-slate-700">
              {peakQuery.data?.best_label && <p>🔥 <strong>{peakQuery.data.best_label}</strong></p>}
              {peakQuery.data?.worst_label && <p className="text-slate-500">💤 {peakQuery.data.worst_label}</p>}
            </div>
          )}
          <div className="overflow-x-auto">
            <div className="min-w-[720px]">
              <div className="mb-1 ml-10 grid gap-0.5 text-center" style={{ gridTemplateColumns: "repeat(24, minmax(0, 1fr))" }}>
                {Array.from({ length: 24 }).map((_, h) => (
                  <span key={h} className="text-[9px] text-slate-400">{h % 3 === 0 ? `${h}h` : ""}</span>
                ))}
              </div>
              {DAYS.map((day, dow) => (
                <div key={day} className="mb-0.5 flex items-center gap-1">
                  <span className="w-9 shrink-0 text-[11px] font-medium text-slate-500">{day}</span>
                  <div className="grid flex-1 gap-0.5" style={{ gridTemplateColumns: "repeat(24, minmax(0, 1fr))" }}>
                    {Array.from({ length: 24 }).map((_, hour) => {
                      const paid = byDowHour.get(`${dow}-${hour}`) ?? 0;
                      return (
                        <div
                          key={hour}
                          title={`${day} ${hour}:00 — ${paid} paid`}
                          aria-label={`${day} ${hour}:00, ${paid} paid payments`}
                          className={`h-5 rounded-sm ${intensity(paid)}`}
                        />
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          </div>
          <p className="mt-3 text-xs text-slate-400">Darker green = more paid orders. Hover any cell for exact counts.</p>
        </CardContent></Card>
      )}
    </div>
  );
};

export default MerchantAnalyticsPeakHours;
