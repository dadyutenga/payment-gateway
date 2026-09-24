import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Building2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";
import { createOrg } from "@/lib/orgApi";

const CreateOrg = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [businessName, setBusinessName] = useState("");
  const [creating, setCreating] = useState(false);

  const handleCreate = async (event: FormEvent) => {
    event.preventDefault();
    setCreating(true);
    try {
      const org = await createOrg({ name: name.trim(), business_name: businessName.trim() || undefined });
      toast.success("Organization created — you are its owner.");
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
      try {
        localStorage.setItem("payments_gateway_org_id", org.id);
      } catch {
        /* storage unavailable — switcher falls back to first org */
      }
      navigate(`/org/${org.id}/members`, { replace: true });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to create organization.");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="flex min-h-[70vh] items-center justify-center px-4">
      <Card className="w-full max-w-md">
        <CardContent className="p-6">
          <div className="flex items-center gap-2">
            <Building2 className="h-5 w-5 text-slate-700" />
            <h1 className="text-lg font-bold text-slate-900">Create your organization</h1>
          </div>
          <p className="mt-1 text-sm text-slate-500">
            Organizations own apps, API keys, and members. You will be its owner.
          </p>
          <form onSubmit={handleCreate} className="mt-5 space-y-4">
            <div>
              <label className="text-sm font-medium text-slate-700">Organization name</label>
              <Input value={name} onChange={(e) => setName(e.target.value)} required placeholder="Acme Ltd" className="mt-1" />
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Business name <span className="font-normal text-slate-400">(optional)</span></label>
              <Input value={businessName} onChange={(e) => setBusinessName(e.target.value)} placeholder="Acme Limited" className="mt-1" />
            </div>
            <Button type="submit" className="w-full" disabled={creating}>
              {creating ? "Creating..." : "Create organization"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
};

export default CreateOrg;
