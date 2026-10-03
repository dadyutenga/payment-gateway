import { useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Banknote, BarChart3, Boxes, Clock,
  CreditCard, Globe, KeyRound, LayoutDashboard, PieChart, Receipt, ScrollText,
  Settings, Terminal, Truck, Users, Wallet, XCircle,
} from "lucide-react";
import { signOut } from "@/lib/auth";
import { listMyOrgs } from "@/lib/orgApi";
import SandboxModeBanner from "@/components/SandboxModeBanner";
import Sidebar, { type SidebarNavGroup } from "@/components/Sidebar";
import BrandMark from "@/components/BrandMark";
import SpaceTopbar from "@/components/SpaceTopbar";
import { TrackNoticeToast } from "@/components/TrackRoute";

const COLLAPSED_KEY = "lipago_sidebar_merchant_collapsed";
const EXPAND_KEY = "lipago_nav_merchant";

// Matches /org/:orgId/<suffix>[/...] for redirect-hub sidebar items whose
// real URL differs from their `to`.
const orgMatch = (suffix: string) => (pathname: string) => {
  const m = pathname.match(/^\/org\/[^/]+\/(.*)$/);
  return !!m && (m[1] === suffix || m[1].startsWith(`${suffix}/`));
};

// Merchant workspace nav: Overview, Payments, Money, Developers,
// Analytics, Team, Settings. Business-only surface.
const NAV_GROUPS: SidebarNavGroup[] = [
  {
    id: "overview", label: "Overview", icon: LayoutDashboard,
    items: [{ to: "/merchant", label: "Home", icon: LayoutDashboard, end: true }],
  },
  {
    id: "payments", label: "Payments", icon: CreditCard,
    items: [
      { to: "/merchant/apps", label: "My Apps", icon: Boxes },
      { to: "/merchant/payments", label: "Payments", icon: Receipt },
    ],
  },
  {
    id: "money", label: "Money", icon: Wallet,
    items: [
      { to: "/merchant/withdrawals", label: "Withdrawals", icon: Banknote },
      { to: "/merchant/settlements", label: "Settlements", icon: ScrollText, match: orgMatch("settlements") },
    ],
  },
  {
    id: "developers", label: "Developers", icon: Terminal,
    items: [
      { to: "/merchant/api-keys", label: "API Keys", icon: KeyRound },
      { to: "/merchant/webhooks", label: "Webhooks", icon: Globe },
      { to: "/merchant/deliveries", label: "Deliveries", icon: Truck },
    ],
  },
  {
    id: "analytics", label: "Analytics", icon: BarChart3,
    items: [
      { to: "/merchant/analytics", label: "Overview", icon: BarChart3, match: orgMatch("analytics") },
      { to: "/merchant/analytics/methods", label: "Methods", icon: PieChart, match: orgMatch("analytics/methods") },
      { to: "/merchant/analytics/peak-hours", label: "Peak Hours", icon: Clock, match: orgMatch("analytics/peak-hours") },
      { to: "/merchant/analytics/customers", label: "Customers", icon: Users, match: orgMatch("analytics/customers") },
      { to: "/merchant/analytics/failures", label: "Failures", icon: XCircle, match: orgMatch("analytics/failures") },
    ],
  },
  {
    id: "team", label: "Team", icon: Users,
    items: [{ to: "/merchant/team", label: "Members", icon: Users, match: orgMatch("members") }],
  },
  {
    id: "settings", label: "Settings", icon: Settings,
    items: [{ to: "/merchant/settings", label: "Settings", icon: Settings, match: orgMatch("settings") }],
  },
];

// Merchant workspace shell: emerald business identity. Never renders
// creator or admin links.
const MerchantLayout = () => {
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSED_KEY) === "1";
    } catch {
      return false;
    }
  });
  const [drawerOpen, setDrawerOpen] = useState(false);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      try {
        localStorage.setItem(COLLAPSED_KEY, prev ? "0" : "1");
      } catch {
        /* ignore */
      }
      return !prev;
    });
  };

  // Close the mobile drawer on navigation and on Escape.
  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);
  useEffect(() => {
    if (!drawerOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setDrawerOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [drawerOpen]);

  // Sandbox/live status reuses the orgs query (shared react-query cache
  // with SandboxModeBanner — one network call).
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 60_000, retry: false });
  const activeOrg = (orgsQuery.data ?? []).find((o) => o.status === "active") ?? orgsQuery.data?.[0];
  const statusBadge = !activeOrg ? undefined : activeOrg.kyc_status === "verified" ? (
    <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-emerald-700">
      Live
    </span>
  ) : (
    <span className="rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-amber-700">
      Sandbox
    </span>
  );

  const brand = (
    <>
      <BrandMark className="h-6 w-6" />
      LipaGO
      <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-emerald-700">
        Merchant
      </span>
    </>
  );

  const sidebar = (
    <Sidebar
      groups={NAV_GROUPS}
      accent="merchant"
      spaceBadge={brand}
      collapsed={collapsed}
      storageKey={EXPAND_KEY}
      onNavigate={() => setDrawerOpen(false)}
      onToggleCollapse={toggleCollapsed}
    />
  );

  return (
    <div className="min-h-screen bg-background lg:flex">
      <TrackNoticeToast />
      <aside
        className={`sticky top-0 hidden h-screen shrink-0 border-t-4 border-emerald-500 transition-[width] lg:block ${
          collapsed ? "w-16" : "w-64"
        }`}
      >
        {sidebar}
      </aside>

      {drawerOpen && (
        <div className="fixed inset-0 z-50 lg:hidden" role="dialog" aria-modal="true" aria-label="Navigation menu">
          <div
            className="absolute inset-0 bg-slate-900/50"
            onClick={() => setDrawerOpen(false)}
            aria-hidden
          />
          <aside className="absolute inset-y-0 left-0 w-72 max-w-[85vw] border-t-4 border-emerald-500 shadow-xl">
            {sidebar}
          </aside>
        </div>
      )}

      <div className="flex min-h-screen min-w-0 flex-1 flex-col">
        <SpaceTopbar
          onMenu={() => setDrawerOpen(true)}
          userLabel="Business account"
          space="customer"
          onSignOut={() => {
            signOut();
            window.location.assign("/login");
          }}
        />
        <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 sm:px-6">
          <SandboxModeBanner track="merchant" />
          <Outlet />
        </main>
      </div>
    </div>
  );
};

export default MerchantLayout;
