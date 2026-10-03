import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, LogOut, Menu, Moon, Sun, X } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { getAdminMe } from "@/lib/adminApi";
import { getOwnProfile } from "@/lib/signupApi";
import { listNotifications, markAllNotificationsRead, markNotificationRead, type DashboardSpace } from "@/lib/notificationsApi";

const THEME_KEY = "lipago_theme";
const PREVIEW_COUNT = 4;

type SpaceTopbarProps = {
  onMenu: () => void;
  userLabel: string;
  space?: DashboardSpace;
  onSignOut: () => void;
};

function relativeTime(value: string) {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
  if (seconds < 60) return "Just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

const SpaceTopbar = ({ onMenu, userLabel, space = "customer", onSignOut }: SpaceTopbarProps) => {
  const queryClient = useQueryClient();
  const panelRef = useRef<HTMLElement>(null);
  const bellRootRef = useRef<HTMLDivElement>(null);
  const bellButtonRef = useRef<HTMLButtonElement>(null);
  const [dark, setDark] = useState(() => {
    try { return localStorage.getItem(THEME_KEY) === "dark"; } catch { return false; }
  });
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const profile = useQuery({
    queryKey: [space === "admin" ? "admin" : "auth", "profile"],
    queryFn: space === "admin" ? getAdminMe : getOwnProfile,
    staleTime: 60_000,
    retry: false,
  });
  const notifications = useQuery({
    queryKey: ["notifications", space],
    queryFn: () => listNotifications(space),
    staleTime: 30_000,
    refetchInterval: 45_000,
    retry: false,
  });
  const markRead = useMutation({
    mutationFn: () => markAllNotificationsRead(space),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["notifications", space] }),
  });
  const identity = profile.data as { full_name?: string; email?: string } | undefined;
  const themeStorageKey = identity?.email ? `${THEME_KEY}:${identity.email.toLowerCase()}` : THEME_KEY;

  useEffect(() => {
    if (!profile.isFetched) return;
    try {
      const stored = localStorage.getItem(themeStorageKey);
      if (stored === "dark" || stored === "light") setDark(stored === "dark");
    } catch { /* storage unavailable */ }
  }, [profile.isFetched, themeStorageKey]);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    if (profile.isFetched) {
      try { localStorage.setItem(themeStorageKey, dark ? "dark" : "light"); } catch { /* storage unavailable */ }
    }
  }, [dark, profile.isFetched, themeStorageKey]);

  useEffect(() => {
    if (!notificationsOpen) return;
    const panel = panelRef.current;
    panel?.querySelector<HTMLElement>("button")?.focus();

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setNotificationsOpen(false);
        return;
      }
      if (event.key !== "Tab" || !panel) return;
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>("button, a, [href], input, select, textarea, [tabindex]:not([tabindex=\"-1\"])")).filter((element) => !element.hasAttribute("disabled"));
      if (!focusable.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    const handlePointerDown = (event: PointerEvent) => {
      if (!bellRootRef.current?.contains(event.target as Node)) setNotificationsOpen(false);
    };

    document.addEventListener("keydown", handleKeyDown);
    document.addEventListener("pointerdown", handlePointerDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.removeEventListener("pointerdown", handlePointerDown);
      bellButtonRef.current?.focus();
    };
  }, [notificationsOpen]);

  const items = notifications.data?.items ?? [];
  const unread = notifications.data?.unread ?? items.filter((item) => !item.read_at).length;
  const visibleItems = items.slice(0, PREVIEW_COUNT);
  const hasMoreItems = (notifications.data?.total ?? items.length) > PREVIEW_COUNT;
  const name = identity?.full_name?.trim().split(/\s+/)[0] || identity?.email || userLabel;
  const notificationsTitleId = `notifications-title-${space}`;

  return (
    <header className="sticky top-0 z-30 flex h-16 shrink-0 items-center justify-between gap-4 border-b border-border bg-background/95 px-4 backdrop-blur sm:px-6">
      <div className="flex min-w-0 items-center gap-3">
        <Button type="button" size="icon" variant="ghost" aria-label="Open navigation menu" onClick={onMenu} className="lg:hidden"><Menu className="h-5 w-5" /></Button>
        <span className="truncate text-sm font-medium text-foreground" aria-label="Signed-in user">{name}</span>
      </div>
      <div className="flex items-center gap-1">
        <Button type="button" variant="ghost" size="icon" aria-label={dark ? "Switch to light theme" : "Switch to dark theme"} onClick={() => setDark((value) => !value)}>{dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}</Button>
        <div ref={bellRootRef} className="relative">
          <Button
            ref={bellButtonRef}
            type="button"
            variant="ghost"
            size="icon"
            aria-label={`Notifications${unread ? `, ${unread} unread` : ""}`}
            aria-haspopup="dialog"
            aria-expanded={notificationsOpen}
            aria-controls={notificationsOpen ? `${notificationsTitleId}-panel` : undefined}
            onClick={() => {
              setNotificationsOpen((value) => !value);
            }}
            className="relative"
          >
            <Bell className="h-4 w-4" />
            {unread > 0 && <span className="absolute right-1 top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[9px] font-bold text-primary-foreground">{unread > 9 ? "9+" : unread}</span>}
          </Button>

          {notificationsOpen && (
            <section
              ref={panelRef}
              id={`${notificationsTitleId}-panel`}
              role="dialog"
              aria-labelledby={notificationsTitleId}
              className="absolute right-0 top-full z-50 mt-2 flex max-h-[calc(100vh-6rem)] w-[min(24rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-xl border border-border bg-background shadow-lg sm:w-96"
            >
              <div className="flex items-center justify-between gap-4 border-b border-border px-4 py-3">
                <h2 id={notificationsTitleId} className="font-semibold text-foreground">Notifications</h2>
                <div className="flex items-center gap-2">
                  <button type="button" className="text-xs font-medium text-primary focus-visible:outline focus-visible:outline-2" onClick={() => markRead.mutate()} disabled={unread === 0 || markRead.isPending}>Mark all as read</button>
                  <Button type="button" variant="ghost" size="icon" aria-label="Close notifications" onClick={() => setNotificationsOpen(false)}><X className="h-4 w-4" /></Button>
                </div>
              </div>
              <div className="min-h-0 overflow-y-auto px-4">
                {notifications.isLoading && <p className="py-6 text-sm text-muted-foreground">Loading notifications…</p>}
                {notifications.isError && <p className="py-6 text-sm text-muted-foreground">Notifications are unavailable right now.</p>}
                {!notifications.isLoading && !notifications.isError && items.length === 0 && <p className="py-6 text-sm text-muted-foreground">You’re all caught up.</p>}
                <ul className="divide-y divide-border">
                  {visibleItems.map((item) => (
                    <li key={item.id} className={`py-3 ${item.read_at ? "" : "bg-muted/40"}`}>
                      <div className="flex gap-3">
                        <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${item.read_at ? "bg-transparent" : "bg-primary"}`} aria-hidden />
                        <div className="min-w-0 flex-1">
                          <p className="text-sm font-semibold text-foreground">{item.emoji ? `${item.emoji} ` : ""}{item.title}</p>
                          <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{item.body}</p>
                          <time className="mt-1.5 block text-xs text-muted-foreground" dateTime={item.created_at}>{relativeTime(item.created_at)}</time>
                          {item.link_url && <Link to={item.link_url} onClick={() => { void markNotificationRead(space, item.id).finally(() => queryClient.invalidateQueries({ queryKey: ["notifications", space] })); setNotificationsOpen(false); }} className="mt-2 inline-block text-xs font-medium text-primary">View update</Link>}
                        </div>
                      </div>
                    </li>
                  ))}
                </ul>
              </div>
              {hasMoreItems && <Link to={space === "admin" ? "/admin/notifications" : "/notifications"} onClick={() => setNotificationsOpen(false)} className="border-t border-border px-4 py-3 text-left text-sm font-medium text-primary hover:bg-muted focus-visible:outline focus-visible:outline-2">View all notifications →</Link>}
            </section>
          )}
        </div>
        <Button type="button" variant="ghost" size="icon" aria-label="Sign out" onClick={onSignOut}><LogOut className="h-4 w-4" /></Button>
      </div>
    </header>
  );
};

export default SpaceTopbar;
