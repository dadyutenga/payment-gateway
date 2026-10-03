import type { LucideIcon } from "lucide-react";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";

export type OutstandingTask = { icon: LucideIcon; label: string; status?: string; action: { label: string; to: string } };
export default function OutstandingTasks({ items }: { items: OutstandingTask[] }) {
  if (!items.length) return null;
  return <section aria-label="Outstanding tasks" className="mt-6 rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-700 dark:bg-slate-900"><h2 className="font-semibold text-slate-900 dark:text-white">A few things to get started</h2><ul className="mt-3 divide-y divide-slate-100 dark:divide-slate-700">{items.map(({ icon: Icon, label, status, action }) => <li key={label} className="flex flex-wrap items-center justify-between gap-3 py-3"><span className="flex items-center gap-3 text-sm text-slate-700 dark:text-slate-200"><Icon className="h-4 w-4 text-slate-500" />{label}{status && <span className="text-xs text-slate-400">{status}</span>}</span><Link className="inline-flex items-center gap-1 text-sm font-medium text-emerald-700 focus-visible:outline focus-visible:outline-2 dark:text-emerald-300" to={action.to}>{action.label}<ArrowRight className="h-3.5 w-3.5" /></Link></li>)}</ul></section>;
}
