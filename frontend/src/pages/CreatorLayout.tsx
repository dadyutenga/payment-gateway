import { useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Banknote, ChevronsLeft, ChevronsRight, HeartHandshake,
  LayoutDashboard, Receipt, Settings,
} from "lucide-react";
import { signOut } from "@/lib/auth";
import { listMyOrgs } from "@/lib/orgApi";
import SandboxModeBanner from "@/components/SandboxModeBanner";
import Sidebar, { type SidebarNavGroup } from "@/components/Sidebar";
import BrandMark from "@/components/BrandMark";
import SpaceTopbar from "@/components/SpaceTopbar";
import { TrackNoticeToast } from "@/components/TrackRoute";

const COLLAPSED_KEY = "lipago_sidebar_creator_collapsed";
const EXPAND_KEY = "lipago_nav_creator";

// Creator workspace nav: Overview, My Page, Payments, Payouts, Settings.
// Personal single-member surface — no Team, no Developers, no full
// analytics suite.
const NAV_GROUPS: SidebarNavGroup[] = [
  {
    id: "overview", label: "Overview", icon: LayoutDashboard,
    items: [{ to: "/creator", label: "Home", icon: LayoutDashboard, end: true }],
  },
  {
    id: "page", label: "My Page", icon: HeartHandshake,
    items: [{ to: "/creator/page", label: "My Page", icon: HeartHandshake }],
  },
  {
    id: "payments", label: "Payments", icon: Receipt,
    items: [{ to: "/creator/payments", label: "Payments", icon: Receipt }],
  },
  {
    id: "money", label: "Money", icon: Banknote,
    items: [{ to: "/creator/payouts", label: "Payouts", icon: Banknote }],
  },
  {
    id: "settings", label: "Settings", icon: Settings,
    items: [{ to: "/creator/settings", label: "Settings", icon: Settings }],
  },
];

// Creator workspace shell: fuchsia personal identity, unmistakable next to
// the emerald merchant shell and the red admin shell. Never renders
// merchant team/developer or admin links.
const CreatorLayout = () => {
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
      <span className="rounded bg-fuchsia-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-fuchsia-700">
        Individual
      </span>
    </>
  );

  const sidebar = (
    <Sidebar
      groups={NAV_GROUPS}
      accent="creator"
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
      <TrackNoticeToast />
      <aside
        className={`sticky top-0 hidden h-screen shrink-0 border-t-4 border-fuchsia-500 transition-[width] lg:block ${
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
          <aside className="absolute inset-y-0 left-0 w-72 max-w-[85vw] border-t-4 border-fuchsia-500 shadow-xl">
            {sidebar}
          </aside>
        </div>
      )}

      <div className="flex min-h-screen min-w-0 flex-1 flex-col">
        <SpaceTopbar
          onMenu={() => setDrawerOpen(true)}
          userLabel="Personal account"
          space="customer"
          onSignOut={() => {
            signOut();
            window.location.assign("/login");
          }}
        />
        <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 sm:px-6">
          <SandboxModeBanner track="creator" />
          <Outlet />
        </main>
      </div>
    </div>
  );
};

export default CreatorLayout;
