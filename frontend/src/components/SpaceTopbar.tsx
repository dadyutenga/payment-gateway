import { useEffect, useState } from "react";
import { Bell, LogOut, Menu, Moon, Sun } from "lucide-react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { getOwnProfile } from "@/lib/signupApi";

const THEME_KEY = "lipago_theme";
const SpaceTopbar = ({ onMenu, brand, statusBadge, userLabel, userLinks, onSignOut }: {
  onMenu: () => void; brand: React.ReactNode; statusBadge?: React.ReactNode; userLabel: string;
  userLinks?: { to: string; label: string }[]; onSignOut: () => void;
}) => {
  const profile = useQuery({ queryKey: ["auth", "profile"], queryFn: getOwnProfile, staleTime: 60_000, retry: false });
  const [dark, setDark] = useState(() => { try { return localStorage.getItem(THEME_KEY) === "dark"; } catch { return false; } });
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const [unread, setUnread] = useState(true);
  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    try { localStorage.setItem(THEME_KEY, dark ? "dark" : "light"); } catch { /* storage unavailable */ }
  }, [dark]);
  useEffect(() => {
    if (!notificationsOpen) return;
    const handle = (event: KeyboardEvent) => { if (event.key === "Escape") setNotificationsOpen(false); };
    window.addEventListener("keydown", handle);
    return () => window.removeEventListener("keydown", handle);
  }, [notificationsOpen]);
  const name = profile.data?.full_name?.trim().split(/\s+/)[0] || profile.data?.email || userLabel;
  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center justify-between gap-2 bg-white px-4 shadow-sm dark:bg-slate-900 sm:px-6">
      <div className="flex min-w-0 items-center gap-3">
        <Button type="button" size="icon" variant="ghost" aria-label="Open navigation menu" onClick={onMenu} className="lg:hidden"><Menu className="h-5 w-5" /></Button>
        <span className="hidden items-center gap-2 font-bold text-slate-900 dark:text-white lg:flex">{brand}</span>
        <span className="truncate text-sm text-slate-600 dark:text-slate-200" aria-label="Signed-in user">{name}</span>{statusBadge}
      </div>
      <div className="flex items-center gap-1">
        <Button type="button" variant="ghost" size="icon" aria-label={dark ? "Switch to light theme" : "Switch to dark theme"} onClick={() => setDark((value) => !value)}>{dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}</Button>
        <Button type="button" variant="ghost" size="icon" aria-label={`Notifications${unread ? ", 1 unread" : ""}`} onClick={() => setNotificationsOpen((value) => !value)} className="relative"><Bell className="h-4 w-4" />{unread && <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-rose-500" />}</Button>
        <DropdownMenu><DropdownMenuTrigger asChild><Button type="button" variant="ghost" size="icon" aria-label="Account menu"><span className="sr-only">Account menu</span><span className="text-xs">•••</span></Button></DropdownMenuTrigger><DropdownMenuContent align="end">{(userLinks ?? []).map((link) => <DropdownMenuItem key={link.to} asChild><Link to={link.to}>{link.label}</Link></DropdownMenuItem>)}<DropdownMenuItem onClick={onSignOut}><LogOut className="mr-2 h-4 w-4" />Sign out</DropdownMenuItem></DropdownMenuContent></DropdownMenu>
      </div>
      {notificationsOpen && <><button className="fixed inset-0 z-40 cursor-default bg-slate-950/10" aria-label="Close notifications" onClick={() => setNotificationsOpen(false)} /><section role="dialog" aria-modal="true" aria-labelledby="notifications-title" className="fixed inset-y-0 right-0 z-50 w-full max-w-sm bg-white p-5 shadow-xl dark:bg-slate-900"><div className="flex items-center justify-between"><h2 id="notifications-title" className="font-semibold">Notifications</h2><button className="text-sm text-emerald-700 focus-visible:outline focus-visible:outline-2" onClick={() => setUnread(false)}>Mark all as read</button></div>{unread ? <article className="mt-5 border-b border-slate-100 py-4 dark:border-slate-700"><div className="flex gap-3"><span className="mt-1 h-2 w-2 shrink-0 rounded-full bg-emerald-500" /><div><p className="text-sm font-medium">Welcome to LipaGO</p><p className="mt-1 text-sm text-slate-500">Your workspace is ready. Check your account setup to get started.</p><time className="mt-2 block text-xs text-slate-400">Just now</time></div></div></article> : <p className="mt-6 text-sm text-slate-500">You’re all caught up.</p>}</section></>}
    </header>
  );
};
export default SpaceTopbar;
