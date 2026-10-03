import { createPortal } from "react-dom";
import { useEffect, useRef, useState } from "react";
import { NavLink, useLocation } from "react-router-dom";
import { ChevronDown, ChevronLeft, ChevronRight, type LucideIcon } from "lucide-react";

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

const SidebarTooltip = ({ label, children }: { label: string; children: React.ReactNode }) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [position, setPosition] = useState({ top: 0, left: 0 });

  const updatePosition = () => {
    const rect = anchorRef.current?.getBoundingClientRect();
    if (rect) setPosition({ top: rect.top + rect.height / 2, left: rect.right + 8 });
  };

  useEffect(() => {
    if (!visible) return;
    updatePosition();
    const handleViewportChange = () => updatePosition();
    window.addEventListener("resize", handleViewportChange);
    window.addEventListener("scroll", handleViewportChange, true);
    return () => {
      window.removeEventListener("resize", handleViewportChange);
      window.removeEventListener("scroll", handleViewportChange, true);
    };
  }, [visible]);

  const show = () => {
    updatePosition();
    setVisible(true);
  };
  const hide = () => setVisible(false);

  return (
    <>
      <div
        ref={anchorRef}
        className="block"
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) hide();
        }}
      >
        {children}
      </div>
      {visible && createPortal(
        <span
          role="tooltip"
          className="pointer-events-none fixed z-[100] -translate-y-1/2 whitespace-nowrap rounded-md bg-popover px-2 py-1 text-xs font-medium text-popover-foreground shadow-md"
          style={{ top: position.top, left: position.left }}
        >
          {label}
        </span>,
        document.body,
      )}
    </>
  );
};

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
  onToggleCollapse,
}: {
  groups: SidebarNavGroup[];
  accent: "merchant" | "creator" | "admin";
  spaceBadge: React.ReactNode;
  collapsed: boolean;
  storageKey: string;
  onNavigate?: () => void;
  onToggleCollapse?: () => void;
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
    accent === "admin"
      ? "bg-red-700 text-white"
      : accent === "creator"
        ? "bg-fuchsia-700 text-white"
        : "bg-slate-900 text-white";

  return (
    <div className="relative h-full w-full">
      <nav
        role="navigation"
        aria-expanded={!collapsed}
        aria-label={typeof spaceBadge === "string" ? spaceBadge : "Primary"}
        className="flex h-full w-full flex-col overflow-y-auto bg-sidebar"
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
                  <SidebarTooltip label={group.label}>
                    <div
                      className="flex justify-center py-1 outline-none"
                      tabIndex={0}
                      role="img"
                      aria-label={group.label}
                      title={group.label}
                    >
                      <GroupIcon className="h-4 w-4 text-slate-400" aria-hidden />
                    </div>
                  </SidebarTooltip>
                )}
                {(open || collapsed) && (
                  <ul className={collapsed ? "space-y-1" : "mb-1 space-y-0.5"}>
                    {group.items.map((item) => {
                      const Icon = item.icon;
                      const active = isItemActive(pathname, item);
                      return (
                        <li key={item.to}>
                          {collapsed ? (
                            <SidebarTooltip label={item.label}>
                              <NavLink
                                to={item.to}
                                end={item.end}
                                aria-current={active ? "page" : undefined}
                                aria-label={item.label}
                                title={item.label}
                                onClick={onNavigate}
                                className={`flex items-center justify-center gap-2.5 rounded-md px-2 py-2 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-sidebar-ring ${
                                  active
                                    ? activeClasses
                                    : "text-slate-600 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                                }`}
                              >
                                <Icon className="h-4 w-4 shrink-0" aria-hidden />
                              </NavLink>
                            </SidebarTooltip>
                          ) : (
                            <NavLink
                              to={item.to}
                              end={item.end}
                              aria-current={active ? "page" : undefined}
                              onClick={onNavigate}
                              className={`flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-sidebar-ring ${
                                active
                                  ? activeClasses
                                  : "text-slate-600 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                              }`}
                            >
                              <Icon className="h-4 w-4 shrink-0" aria-hidden />
                              <span className="truncate">{item.label}</span>
                            </NavLink>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                )}
              </div>
            );
          })}
        </div>
      </nav>

      {onToggleCollapse && (
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-expanded={!collapsed}
          className="absolute right-0 top-20 z-30 hidden h-7 w-7 translate-x-1/2 items-center justify-center rounded-full border border-sidebar-border bg-sidebar text-sidebar-foreground shadow-sm transition-colors hover:bg-sidebar-accent focus-visible:outline-2 focus-visible:outline-sidebar-ring lg:flex"
        >
          {collapsed ? <ChevronRight className="h-4 w-4" aria-hidden /> : <ChevronLeft className="h-4 w-4" aria-hidden />}
        </button>
      )}
    </div>
  );
};

export default Sidebar;
