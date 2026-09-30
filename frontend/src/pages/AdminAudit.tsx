import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listAudit } from "@/lib/adminOrgApi";

// Operator audit trail viewer: who did what, to which target, when.
// Reads only — every admin mutation across the app writes here.
const AdminAudit = () => {
  const [action, setAction] = useState("");
  const [actor, setActor] = useState("");
  const [applied, setApplied] = useState({ action: "", actor: "" });
  const [offset, setOffset] = useState(0);
  const limit = 50;

  const auditQuery = useQuery({
    queryKey: ["admin", "audit", applied.action, applied.actor, offset],
    queryFn: () => listAudit(
      applied.action || undefined,
      applied.actor || undefined,
      limit,
      offset,
    ),
    staleTime: 15_000,
  });
  const entries = auditQuery.data ?? [];

  const apply = (e: React.FormEvent) => {
    e.preventDefault();
    setOffset(0);
    setApplied({ action: action.trim(), actor: actor.trim() });
  };

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Audit log</h2>
        <p className="mt-1 text-sm text-slate-500">Every admin mutation — actor, action, target, timestamp.</p>
      </div>

      <form onSubmit={apply} className="mt-4 flex flex-wrap items-end gap-2">
        <div>
          <label className="text-xs font-medium text-slate-600">Action contains</label>
          <Input value={action} onChange={(e) => setAction(e.target.value)} placeholder="kyc.approve" className="mt-1 h-9 w-44" />
        </div>
        <div>
          <label className="text-xs font-medium text-slate-600">Actor email contains</label>
          <Input value={actor} onChange={(e) => setActor(e.target.value)} placeholder="ops@example.com" className="mt-1 h-9 w-52" />
        </div>
        <Button type="submit" size="sm">Filter</Button>
      </form>

      <Card className="mt-4"><CardContent className="overflow-x-auto p-0">
        {auditQuery.isLoading ? (
          <div className="p-4"><Skeleton className="h-40 w-full" /></div>
        ) : auditQuery.error ? (
          <p className="p-4 text-sm text-red-600">Unable to load audit log.</p>
        ) : (
          <Table>
            <TableHeader><TableRow>
              <TableHead>Time</TableHead><TableHead>Actor</TableHead>
              <TableHead>Action</TableHead><TableHead>Target</TableHead><TableHead>IP</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {entries.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className="whitespace-nowrap text-xs text-slate-500">
                    {new Date(e.created_at).toLocaleString()}
                  </TableCell>
                  <TableCell className="max-w-xs truncate text-xs">{e.actor_email}</TableCell>
                  <TableCell className="text-xs font-medium">{e.action}</TableCell>
                  <TableCell className="max-w-xs truncate font-mono text-xs text-slate-500">
                    {e.target_type} {e.target_id.slice(0, 8)}
                  </TableCell>
                  <TableCell className="text-xs text-slate-400">{e.ip || "—"}</TableCell>
                </TableRow>
              ))}
              {entries.length === 0 && (
                <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No audit entries match.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        )}
      </CardContent></Card>

      <div className="mt-3 flex items-center gap-2">
        <Button size="sm" variant="outline" disabled={offset === 0} onClick={() => setOffset((o) => Math.max(0, o - limit))}>
          Previous
        </Button>
        <span className="text-xs text-slate-500">Showing {entries.length} from offset {offset}</span>
        <Button size="sm" variant="outline" disabled={entries.length < limit} onClick={() => setOffset((o) => o + limit)}>
          Next
        </Button>
      </div>
    </div>
  );
};

export default AdminAudit;
