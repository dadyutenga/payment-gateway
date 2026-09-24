import { FormEvent, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { UserPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import {
  acceptOrgInvite,
  changeOrgMemberRole,
  getOrg,
  inviteOrgMember,
  leaveOrg,
  listOrgMembers,
  removeOrgMember,
  ORG_ROLES,
  OrgApiError,
  type OrgRole,
} from "@/lib/orgApi";
import { getAccessToken } from "@/lib/auth";

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

function currentUserId(): string {
  try {
    const token = getAccessToken();
    if (!token) return "";
    const payload = JSON.parse(atob(token.split(".")[1]));
    return typeof payload.sub === "string" ? payload.sub : "";
  } catch {
    return "";
  }
}

const OrgMembers = () => {
  const { orgId = "" } = useParams();
  const queryClient = useQueryClient();
  const me = currentUserId();

  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000 });
  const membersQuery = useQuery({
    queryKey: ["orgs", orgId, "members"],
    queryFn: () => listOrgMembers(orgId),
    staleTime: 15_000,
  });

  const myRole = orgQuery.data?.role;
  const isOwner = myRole === "owner";
  const members = membersQuery.data ?? [];
  const invited = members.filter((m) => m.status === "invited");
  const active = members.filter((m) => m.status === "active");
  const myInvite = invited.find((m) => m.user_id === me);

  const [email, setEmail] = useState("");
  const [role, setRole] = useState<OrgRole>("developer");
  const [inviting, setInviting] = useState(false);
  const [actingId, setActingId] = useState<string | null>(null);

  const reload = () => {
    queryClient.invalidateQueries({ queryKey: ["orgs", orgId, "members"] });
    queryClient.invalidateQueries({ queryKey: ["orgs", orgId] });
  };

  const handleInvite = async (event: FormEvent) => {
    event.preventDefault();
    setInviting(true);
    try {
      await inviteOrgMember(orgId, { email: email.trim(), role });
      toast.success("Invite sent — they join once they accept.");
      setEmail("");
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to invite member."));
    } finally {
      setInviting(false);
    }
  };

  const handleAccept = async () => {
    try {
      await acceptOrgInvite(orgId);
      toast.success("Invite accepted — welcome.");
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to accept invite."));
    }
  };

  const handleRoleChange = async (userId: string, next: OrgRole) => {
    setActingId(userId);
    try {
      await changeOrgMemberRole(orgId, userId, next);
      toast.success("Role updated.");
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to change role."));
    } finally {
      setActingId(null);
    }
  };

  const handleRemove = async (userId: string, label: string) => {
    if (!window.confirm(`Remove ${label} from this organization?`)) return;
    setActingId(userId);
    try {
      await removeOrgMember(orgId, userId);
      toast.success("Member removed.");
      reload();
    } catch (err) {
      if (err instanceof OrgApiError && err.code === "last_owner") {
        toast.error("The organization must keep at least one owner.");
      } else {
        toast.error(errorMessage(err, "Unable to remove member."));
      }
    } finally {
      setActingId(null);
    }
  };

  const handleLeave = async () => {
    if (!window.confirm("Leave this organization?")) return;
    try {
      await leaveOrg(orgId);
      toast.success("You left the organization.");
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
      window.location.assign("/merchant/apps");
    } catch (err) {
      toast.error(errorMessage(err, "Unable to leave."));
    }
  };

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Members — {orgQuery.data?.name ?? "…"}</h2>
          <p className="mt-1 text-sm text-slate-500">
            Invite by email (they need an account first), change roles, or remove members.
            {myRole && <> Your role: <Badge variant="secondary">{myRole}</Badge></>}
          </p>
        </div>
        {me && active.some((m) => m.user_id === me) && (
          <Button size="sm" variant="outline" onClick={handleLeave}>Leave org</Button>
        )}
      </div>

      {myInvite && (
        <Card className="mt-4 border-blue-200 bg-blue-50">
          <CardContent className="flex flex-col gap-2 p-4 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm text-slate-700">You are invited as <strong>{myInvite.role}</strong>. Accept to get access.</p>
            <Button size="sm" onClick={handleAccept}>Accept invite</Button>
          </CardContent>
        </Card>
      )}

      {isOwner && (
        <Card className="mt-4">
          <CardContent className="p-4">
            <form onSubmit={handleInvite} className="flex flex-col gap-2 sm:flex-row sm:items-end">
              <div className="flex-1">
                <label className="text-sm font-medium text-slate-700">Invite by email</label>
                <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required placeholder="teammate@example.com" className="mt-1" />
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Role</label>
                <select
                  className="mt-1 h-10 rounded-md border border-slate-300 px-2 text-sm"
                  value={role}
                  onChange={(e) => setRole(e.target.value as OrgRole)}
                >
                  {ORG_ROLES.map((r) => (
                    <option key={r.value} value={r.value} title={r.description}>{r.label}</option>
                  ))}
                </select>
              </div>
              <Button type="submit" disabled={inviting}><UserPlus className="h-3.5 w-3.5 mr-1" /> {inviting ? "Inviting..." : "Invite"}</Button>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="mt-4 space-y-2">
        {membersQuery.isLoading && <Skeleton className="h-14 w-full" />}
        {invited.length > 0 && (
          <>
            <p className="text-xs font-semibold uppercase tracking-wide text-slate-400">Pending invites</p>
            {invited.map((m) => (
              <Card key={m.user_id}>
                <CardContent className="flex items-center justify-between gap-2 p-3">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-slate-900">{m.email || m.user_id}</p>
                    <p className="text-xs text-slate-400">invited · awaiting accept</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Badge variant="secondary">{m.role}</Badge>
                    <Badge variant="outline">invited</Badge>
                  </div>
                </CardContent>
              </Card>
            ))}
          </>
        )}
        <p className="text-xs font-semibold uppercase tracking-wide text-slate-400">Active members</p>
        {active.map((m) => (
          <Card key={m.user_id}>
            <CardContent className="flex items-center justify-between gap-2 p-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-slate-900">{m.full_name || m.email || m.user_id}</p>
                <p className="truncate text-xs text-slate-500">{m.email}</p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {isOwner && m.user_id !== me ? (
                  <select
                    className="h-8 rounded-md border border-slate-300 px-1.5 text-xs"
                    value={m.role}
                    disabled={actingId === m.user_id}
                    onChange={(e) => handleRoleChange(m.user_id, e.target.value as OrgRole)}
                  >
                    {ORG_ROLES.map((r) => (
                      <option key={r.value} value={r.value}>{r.label}</option>
                    ))}
                  </select>
                ) : (
                  <Badge variant="secondary">{m.role}</Badge>
                )}
                {isOwner && m.user_id !== me && (
                  <Button size="sm" variant="outline" disabled={actingId === m.user_id} onClick={() => handleRemove(m.user_id, m.email || m.user_id)}>
                    Remove
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>
        ))}
        {!membersQuery.isLoading && members.length === 0 && (
          <p className="rounded-lg bg-slate-50 px-3 py-4 text-center text-sm text-slate-500">No members yet.</p>
        )}
      </div>
    </div>
  );
};

export default OrgMembers;
