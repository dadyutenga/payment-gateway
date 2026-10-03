import type { LucideIcon } from "lucide-react";
import { ArrowRight, CheckCircle2 } from "lucide-react";
import { Link } from "react-router-dom";

export type OutstandingTask = { icon: LucideIcon; label: string; status?: string; action: { label: string; to: string } };
export default function OutstandingTasks({ items }: { items: OutstandingTask[] }) {
  if (!items.length) return null;
  return (
    <section aria-label="Outstanding tasks" className="mt-6 rounded-xl border border-border bg-card p-4 shadow-sm">
      <div className="flex items-center gap-2">
        <CheckCircle2 className="h-4 w-4 text-muted-foreground" aria-hidden />
        <h2 className="font-semibold text-card-foreground">A few things to get started</h2>
      </div>
      <ul className="mt-3 divide-y divide-border">
        {items.map(({ icon: Icon, label, status, action }) => (
          <li key={label} className="flex flex-wrap items-center justify-between gap-3 py-3 first:pt-2 last:pb-1">
            <span className="flex items-center gap-3 text-sm text-foreground">
              <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted text-muted-foreground"><Icon className="h-4 w-4" aria-hidden /></span>
              <span>{label}{status && <span className="ml-2 text-xs text-muted-foreground">{status}</span>}</span>
            </span>
            <Link className="inline-flex items-center gap-1 text-sm font-medium text-primary focus-visible:outline focus-visible:outline-2" to={action.to}>
              {action.label}<ArrowRight className="h-3.5 w-3.5" aria-hidden />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
