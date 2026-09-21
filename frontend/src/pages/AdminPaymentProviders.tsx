import { FormEvent, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Pencil, Trash2, Star } from "lucide-react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { toast } from "@/components/ui/sonner";
import {
  createPaymentProvider,
  deletePaymentProvider,
  listPaymentProviders,
  setDefaultPaymentProvider,
  updatePaymentProvider,
  type PaymentProviderAccount,
} from "@/lib/adminApi";

const PROVIDER_KINDS = [{ value: "sonicpesa", label: "SonicPesa" }];

const CREDENTIAL_FIELDS: Record<string, { key: string; label: string; type?: string }[]> = {
  sonicpesa: [
    { key: "api_key", label: "API key", type: "password" },
    { key: "api_secret", label: "API secret", type: "password" },
  ],
};

const blankForm = {
  provider: "sonicpesa",
  name: "",
  environment: "production",
  baseUrl: "",
  credentials: {} as Record<string, string>,
  status: "active",
};

function formatDate(value: string) {
  return new Date(value).toLocaleString();
}

const AdminPaymentProviders = () => {
  const queryClient = useQueryClient();
  const runWithStepUp = <T,>(action: () => Promise<T>) => action();
  const { data: providers = [], isLoading: loading, error } = useQuery({
    queryKey: ["admin", "payment-providers"],
    queryFn: () => listPaymentProviders().then((data) => (Array.isArray(data) ? data : [])),
    staleTime: 15_000,
  });

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState(blankForm);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (error) toast.error(error instanceof Error ? error.message : "Unable to load payment providers.");
  }, [error]);

  const reload = () => queryClient.invalidateQueries({ queryKey: ["admin", "payment-providers"] });

  const openCreate = () => {
    setEditingId(null);
    setForm(blankForm);
    setDialogOpen(true);
  };

  const openEdit = (account: PaymentProviderAccount) => {
    setEditingId(account.id);
    setForm({
      provider: account.provider,
      name: account.name,
      environment: account.environment,
      baseUrl: account.base_url ?? "",
      credentials: {},
      status: account.status,
    });
    setDialogOpen(true);
  };

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      if (editingId) {
        await runWithStepUp(() =>
          updatePaymentProvider(editingId, {
            name: form.name,
            environment: form.environment,
            base_url: form.baseUrl || undefined,
            status: form.status,
            credentials: Object.keys(form.credentials).length > 0 ? form.credentials : undefined,
          }),
        );
        toast.success("Provider updated.");
      } else {
        await runWithStepUp(() =>
          createPaymentProvider({
            provider: form.provider,
            name: form.name,
            environment: form.environment,
            base_url: form.baseUrl || undefined,
            credentials: form.credentials,
          }),
        );
        toast.success("Provider created.");
      }
      setDialogOpen(false);
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to save provider.");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Delete this payment provider? This cannot be undone.")) return;
    try {
      await runWithStepUp(() => deletePaymentProvider(id));
      toast.success("Provider deleted.");
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to delete provider.");
    }
  };

  const handleSetDefault = async (id: string) => {
    try {
      await setDefaultPaymentProvider(id);
      toast.success("Default provider updated.");
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to set default provider.");
    }
  };

  const fields = CREDENTIAL_FIELDS[form.provider] ?? [];

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Payment providers</h2>
          <p className="mt-1 text-sm text-slate-500">
            Register payment gateway accounts. Credentials are encrypted at rest and never shown again after entry.
          </p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button className="w-full sm:w-auto" onClick={openCreate}><Plus className="h-4 w-4 mr-1" /> New provider</Button>
          </DialogTrigger>
          <DialogContent className="max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>{editingId ? "Edit provider" : "Add provider"}</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleSubmit} className="space-y-4">
              {!editingId && (
                <div>
                  <label className="text-sm font-medium text-slate-700">Provider</label>
                  <select
                    className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                    value={form.provider}
                    onChange={(e) => setForm((f) => ({ ...f, provider: e.target.value, credentials: {} }))}
                  >
                    {PROVIDER_KINDS.map((k) => (
                      <option key={k.value} value={k.value}>{k.label}</option>
                    ))}
                  </select>
                </div>
              )}
              <div>
                <label className="text-sm font-medium text-slate-700">Label</label>
                <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} required className="mt-1" placeholder="e.g. SonicPesa (production)" />
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Environment</label>
                <select
                  className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                  value={form.environment}
                  onChange={(e) => setForm((f) => ({ ...f, environment: e.target.value }))}
                >
                  <option value="production">Production</option>
                  <option value="sandbox">Sandbox</option>
                </select>
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Base URL (optional)</label>
                <Input
                  value={form.baseUrl}
                  onChange={(e) => setForm((f) => ({ ...f, baseUrl: e.target.value }))}
                  className="mt-1"
                  placeholder="Leave blank to use the provider default"
                />
              </div>
              {fields.map((field) => (
                <div key={field.key}>
                  <label className="text-sm font-medium text-slate-700">{field.label}</label>
                  <Input
                    type={field.type ?? "text"}
                    value={form.credentials[field.key] ?? ""}
                    onChange={(e) => setForm((f) => ({ ...f, credentials: { ...f.credentials, [field.key]: e.target.value } }))}
                    required={!editingId}
                    placeholder={editingId ? "Leave blank to keep existing" : undefined}
                    className="mt-1"
                  />
                </div>
              ))}
              {editingId && (
                <div>
                  <label className="text-sm font-medium text-slate-700">Status</label>
                  <select
                    className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                    value={form.status}
                    onChange={(e) => setForm((f) => ({ ...f, status: e.target.value }))}
                  >
                    <option value="active">Active</option>
                    <option value="inactive">Inactive</option>
                  </select>
                </div>
              )}
              <DialogFooter>
                <Button type="submit" disabled={saving}>{saving ? "Saving..." : "Save provider"}</Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Table className="mt-6">
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Provider</TableHead>
            <TableHead>Environment</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Created</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {providers.map((account) => (
            <TableRow key={account.id}>
              <TableCell className="font-medium">
                {account.name}
                {account.is_default && <Badge className="ml-2" variant="default">Default</Badge>}
              </TableCell>
              <TableCell className="text-slate-500">{account.provider}</TableCell>
              <TableCell><Badge variant="secondary">{account.environment}</Badge></TableCell>
              <TableCell>
                <Badge variant={account.status === "active" ? "default" : "secondary"}>{account.status}</Badge>
              </TableCell>
              <TableCell>{formatDate(account.created_at)}</TableCell>
              <TableCell className="flex items-center gap-2 justify-end">
                {!account.is_default && (
                  <Button size="icon" variant="outline" title="Set as default" onClick={() => handleSetDefault(account.id)}>
                    <Star className="h-4 w-4" />
                  </Button>
                )}
                <Button size="icon" variant="outline" onClick={() => openEdit(account)}><Pencil className="h-4 w-4" /></Button>
                <Button size="icon" variant="outline" onClick={() => handleDelete(account.id)}><Trash2 className="h-4 w-4" /></Button>
              </TableCell>
            </TableRow>
          ))}
          {!loading && providers.length === 0 && (
            <TableRow><TableCell colSpan={6} className="text-center text-slate-500">No payment providers registered yet.</TableCell></TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
};

export default AdminPaymentProviders;
