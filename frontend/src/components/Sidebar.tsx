import { useEffect, useState } from "react";
import { NavLink, useLocation } from "react-router-dom";
import { ChevronDown, type LucideIcon } from "lucide-react";

export type SidebarNavItem = {
  to: string;
  label: string;
  icon: LucideIcon;
  end?: boolean;
  /** Custom active matcher (for redirect hubs whose real URL differs). */
  match?: (pathname: string) => boolean;
};

export type SidebarNavGroup = {
  id: string;
  label: string;
  icon: LucideIcon;
  items: SidebarNavItem[];
};

function isItemActive(pathname: string, item: SidebarNavItem) {
  if (item.match) return item.match(pathname);
  if (item.end) return pathname === item.to;
  return pathname === item.to || pathname.startsWith(item.to + "/");
}

// Shared sidebar, parameterized by nav config — merchant and admin pass
// their own trees into the same component. Styling hooks off the existing
// sidebar theme tokens so light/dark keeps working.
const Sidebar = ({
  groups,
  accent,
  spaceBadge,
  collapsed,
  storageKey,
  onNavigate,
  footer,
}: {
  groups: SidebarNavGroup[];
  accent: "merchant" | "admin";
  spaceBadge: React.ReactNode;
  collapsed: boolean;
  storageKey: string;
  onNavigate?: () => void;
  footer?: React.ReactNode;
}) => {
  const location = useLocation();
  const pathname = location.pathname;

  const activeGroupId = groups.find((g) => g.items.some((item) => isItemActive(pathname, item)))?.id;

  const [expanded, setExpanded] = useState<Record<string, boolean>>(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      if (raw) return JSON.parse(raw) as Record<string, boolean>;
    } catch {
      /* ignore */
    }
    return {};
  });

  // Auto-expand the active group on navigation (persisted user choices
  // otherwise win).
  useEffect(() => {
    if (activeGroupId) {
      setExpanded((prev) => {
        if (prev[activeGroupId]) return prev;
        const next = { ...prev, [activeGroupId]: true };
        try {
          localStorage.setItem(storageKey, JSON.stringify(next));
        } catch {
          /* ignore */
        }
        return next;
      });
    }
  }, [activeGroupId, storageKey]);

  const toggleGroup = (id: string) => {
    setExpanded((prev) => {
      const next = { ...prev, [id]: !(prev[id] ?? true) };
      try {
        localStorage.setItem(storageKey, JSON.stringify(next));
      } catch {
        /* ignore */
      }
      return next;
    });
  };

  const isOpen = (id: string) => expanded[id] ?? true;
  const activeClasses =
    accent === "admin" ? "bg-red-700 text-white" : "bg-slate-900 text-white";

  return (
    <nav
      role="navigation"
      aria-label={typeof spaceBadge === "string" ? spaceBadge : "Primary"}
      className="flex h-full w-full flex-col overflow-y-auto border-r border-sidebar-border bg-sidebar"
    >
      <div className="flex items-center gap-2 px-4 pb-2 pt-4 font-bold text-sidebar-foreground">
        {!collapsed && spaceBadge}
        {collapsed && <span className="sr-only">Primary navigation</span>}
      </div>

      <div className="flex-1 space-y-1 px-2 pb-4">
        {groups.map((group) => {
          const open = isOpen(group.id);
          const GroupIcon = group.icon;
          return (
            <div key={group.id}>
              {!collapsed ? (
                <button
                  type="button"
                  aria-expanded={open}
                  onClick={() => toggleGroup(group.id)}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-[11px] font-bold uppercase tracking-wide text-slate-500 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-sidebar-ring"
                >
                  <GroupIcon className="h-3.5 w-3.5 shrink-0" aria-hidden />
                  <span className="flex-1 text-left">{group.label}</span>
                  <ChevronDown
                    className={`h-3.5 w-3.5 shrink-0 transition-transform ${open ? "" : "-rotate-90"}`}
                    aria-hidden
                  />
                </button>
              ) : (
                <div className="flex justify-center py-1" title={group.label}>
                  <GroupIcon className="h-4 w-4 text-slate-400" aria-hidden />
                </div>
              )}
              {(open || collapsed) && (
                <ul className={collapsed ? "space-y-1" : "mb-1 space-y-0.5"}>
                  {group.items.map((item) => {
                    const Icon = item.icon;
                    const active = isItemActive(pathname, item);
                    return (
                      <li key={item.to}>
                        <NavLink
                          to={item.to}
                          end={item.end}
                          aria-current={active ? "page" : undefined}
                          title={collapsed ? item.label : undefined}
                          onClick={onNavigate}
                          className={`flex items-center gap-2.5 rounded-md text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-sidebar-ring ${
                            collapsed ? "justify-center px-2 py-2" : "px-2.5 py-1.5"
                          } ${
                            active
                              ? activeClasses
                              : "text-slate-600 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                          }`}
                        >
                          <Icon className="h-4 w-4 shrink-0" aria-hidden />
                          {!collapsed && <span className="truncate">{item.label}</span>}
                        </NavLink>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          );
        })}
      </div>

      {footer && !collapsed && (
        <div className="border-t border-sidebar-border p-3 text-xs text-slate-500">{footer}</div>
      )}
    </nav>
  );
};

export default Sidebar;
