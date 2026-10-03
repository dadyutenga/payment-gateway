import { useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Activity, BarChart3, Building2, ChevronsLeft, ChevronsRight, CreditCard, FileCheck, History,
  LayoutDashboard, Wallet, XCircle,
} from "lucide-react";
import { signOut } from "@/lib/auth";
import { getAdminMe } from "@/lib/adminApi";
import Sidebar, { type SidebarNavGroup } from "@/components/Sidebar";
import BrandMark from "@/components/BrandMark";
import SpaceTopbar from "@/components/SpaceTopbar";

const COLLAPSED_KEY = "lipago_sidebar_admin_collapsed";
const EXPAND_KEY = "lipago_nav_admin";

const BASE_GROUPS: SidebarNavGroup[] = [
  {
    id: "overview", label: "Overview", icon: LayoutDashboard,
    items: [{ to: "/admin", label: "Home", icon: LayoutDashboard, end: true }],
  },
  {
    id: "organizations", label: "Organizations", icon: Building2,
    items: [{ to: "/admin/kyc", label: "KYC review", icon: FileCheck }],
  },
  {
    id: "analytics", label: "Analytics", icon: BarChart3,
    items: [
      { to: "/admin/analytics", label: "Overview", icon: BarChart3 },
      { to: "/admin/analytics/providers", label: "Provider stats", icon: BarChart3 },
      { to: "/admin/analytics/merchants", label: "Merchants", icon: Building2 },
      { to: "/admin/analytics/failures", label: "Failures", icon: XCircle },
    ],
  },
  {
    id: "ops", label: "Ops", icon: Activity,
    items: [{ to: "/admin/ops", label: "Ops", icon: Activity }],
  },
  {
    id: "payments", label: "Payments", icon: CreditCard,
    items: [
      { to: "/admin/payments", label: "Payments", icon: CreditCard },
      { to: "/admin/payments/apps", label: "Apps", icon: Building2 },
      { to: "/admin/payments/withdrawals", label: "Withdrawals", icon: Wallet },
      { to: "/admin/payments/providers", label: "Pay providers", icon: Wallet },
    ],
  },
  {
    id: "audit", label: "Audit", icon: History,
    items: [{ to: "/admin/audit", label: "Audit log", icon: History }],
  },
];

// Operator-only layout: red-accented sidebar, slim top bar with ADMIN
// identity. The backend re-checks admin on every request regardless.
const AdminLayout = () => {
  const location = useLocation();
  // Admin-only links stay hidden until the backend confirms the operator
  // session — it re-checks admin on every request regardless.
  const meQuery = useQuery({ queryKey: ["admin", "me"], queryFn: () => getAdminMe(), staleTime: 60_000, retry: false });
  const isAdmin = meQuery.data?.is_admin ?? false;

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

  const groups = BASE_GROUPS.filter((g) => g.id !== "organizations" || isAdmin);

  const brand = (
    <>
      <BrandMark className="h-6 w-6" />
      LipaGO
      <span className="rounded bg-red-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-red-700">
        Admin
      </span>
    </>
  );

  const sidebar = (
    <Sidebar
      groups={groups}
      accent="admin"
      spaceBadge={brand}
      collapsed={collapsed}
      storageKey={EXPAND_KEY}
      onNavigate={() => setDrawerOpen(false)}
      footer={
        <button
          type="button"
          onClick={toggleCollapsed}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className="hidden w-full items-center justify-center gap-1 rounded-md px-2 py-1.5 text-slate-500 hover:bg-sidebar-accent lg:flex"
        >
          {collapsed ? <ChevronsRight className="h-4 w-4" /> : <><ChevronsLeft className="h-4 w-4" /> Collapse</>}
        </button>
      }
    />
  );

  return (
    <div className="min-h-screen bg-background lg:flex">
      <aside
        className={`sticky top-0 hidden h-screen shrink-0 transition-[width] lg:block ${
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
          <aside className="absolute inset-y-0 left-0 w-72 max-w-[85vw] shadow-xl">
            {sidebar}
          </aside>
        </div>
      )}

      <div className="flex min-h-screen min-w-0 flex-1 flex-col">
        <SpaceTopbar
          onMenu={() => setDrawerOpen(true)}
          userLabel="Operator"
          space="admin"
          onSignOut={() => {
            signOut();
            window.location.assign("/admin/login");
          }}
        />
        <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 sm:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
};

export default AdminLayout;
