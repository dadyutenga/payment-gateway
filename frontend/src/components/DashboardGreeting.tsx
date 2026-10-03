import { useQuery } from "@tanstack/react-query";
import { getAdminMe } from "@/lib/adminApi";
import { getOwnProfile } from "@/lib/signupApi";

type GreetingSpace = "customer" | "admin";

function firstName(fullName?: string, email?: string, space: GreetingSpace = "customer") {
  const name = fullName?.trim().split(/\s+/)[0];
  if (name) return name;
  if (space === "admin") return "Operator";
  return email?.trim() || "there";
}

export default function DashboardGreeting({ space = "customer", description }: { space?: GreetingSpace; description: string }) {
  const profile = useQuery({
    queryKey: [space === "admin" ? "admin" : "auth", "profile"],
    queryFn: space === "admin" ? getAdminMe : getOwnProfile,
    staleTime: 60_000,
    retry: false,
  });
  const identity = profile.data as { full_name?: string; email?: string } | undefined;
  const name = firstName(identity?.full_name, identity?.email, space);

  return (
    <div>
      <h1 className="text-2xl font-bold tracking-tight text-foreground">Hello, {name}. Welcome to LipaGO.</h1>
      <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{description}</p>
    </div>
  );
}
