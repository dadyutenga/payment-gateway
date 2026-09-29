import { useState } from "react";
import { Download } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";
import { downloadReportCSV, type AnalyticsQuery } from "@/lib/analyticsApi";

export type RangePreset = "today" | "7d" | "30d" | "mtd" | "custom";

function eatToday(): string {
  const fmt = new Intl.DateTimeFormat("en-CA", { timeZone: "Africa/Dar_es_Salaam", year: "numeric", month: "2-digit", day: "2-digit" });
  return fmt.format(new Date());
}

function addDays(iso: string, days: number): string {
  const d = new Date(`${iso}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

export function presetRange(preset: Exclude<RangePreset, "custom">): { from: string; to: string } {
  const today = eatToday();
  switch (preset) {
    case "today":
      return { from: today, to: today };
    case "7d":
      return { from: addDays(today, -6), to: today };
    case "mtd":
      return { from: today.slice(0, 8) + "01", to: today };
    case "30d":
    default:
      return { from: addDays(today, -29), to: today };
  }
}

export function DeltaBadge({ pct }: { pct?: number }) {
  if (pct === undefined || pct === null || !Number.isFinite(pct)) {
    return <span className="text-xs text-slate-400">n/a vs prev</span>;
  }
  const up = pct >= 0;
  return (
    <span className={`text-xs font-semibold ${up ? "text-emerald-600" : "text-rose-600"}`}>
      {up ? "▲" : "▼"} {Math.abs(pct).toFixed(1)}% vs prev
    </span>
  );
}

export function moneyText(m?: Record<string, string>): string {
  const entries = Object.entries(m ?? {});
  if (entries.length === 0) return "—";
  return entries.map(([c, v]) => `${Number(v).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ${c}`).join(" · ");
}

export function rateText(r?: number | null): string {
  if (r === undefined || r === null || !Number.isFinite(r)) return "—";
  return `${(r * 100).toFixed(1)}%`;
}

export function CsvButton({ report, query }: { report: string; query?: AnalyticsQuery }) {
  const [busy, setBusy] = useState(false);
  const handle = async () => {
    setBusy(true);
    try {
      await downloadReportCSV(report, query);
      toast.success("CSV downloaded.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to build export.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button size="sm" variant="outline" disabled={busy} onClick={handle}>
      <Download className="h-3.5 w-3.5 mr-1" /> {busy ? "Building..." : "CSV"}
    </Button>
  );
}

export function DateRangePicker({
  from, to, onChange,
}: {
  from: string;
  to: string;
  onChange: (from: string, to: string) => void;
}) {
  const [preset, setPreset] = useState<RangePreset>("30d");
  const pick = (p: Exclude<RangePreset, "custom">) => {
    setPreset(p);
    const r = presetRange(p);
    onChange(r.from, r.to);
  };
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {(["today", "7d", "30d", "mtd"] as const).map((p) => (
        <Button key={p} size="sm" variant={preset === p ? "default" : "outline"} onClick={() => pick(p)}>
          {p === "mtd" ? "MTD" : p}
        </Button>
      ))}
      <Input
        type="date"
        aria-label="From date"
        value={from}
        onChange={(e) => { setPreset("custom"); onChange(e.target.value, to); }}
        className="h-8 w-36"
      />
      <span className="text-xs text-slate-400">→</span>
      <Input
        type="date"
        aria-label="To date"
        value={to}
        onChange={(e) => { setPreset("custom"); onChange(from, e.target.value); }}
        className="h-8 w-36"
      />
      <span className="text-[11px] text-slate-400">EAT</span>
    </div>
  );
}
