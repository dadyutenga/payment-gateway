import { LogOut, Menu } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

// Slim top bar: hamburger (below lg), space identity, optional status
// badge (sandbox/live), and the user menu. Primary navigation lives in
// the Sidebar — this bar only holds controls.
const SpaceTopbar = ({
  onMenu,
  brand,
  statusBadge,
  userLabel,
  userLinks,
  onSignOut,
}: {
  onMenu: () => void;
  brand: React.ReactNode;
  statusBadge?: React.ReactNode;
  userLabel: string;
  userLinks?: { to: string; label: string }[];
  onSignOut: () => void;
}) => {
  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center justify-between gap-2 border-b border-slate-200 bg-white px-4 sm:px-6">
      <div className="flex items-center gap-2">
        <Button
          type="button"
          size="icon"
          variant="ghost"
          aria-label="Open navigation menu"
          onClick={onMenu}
          className="lg:hidden"
        >
          <Menu className="h-5 w-5" />
        </Button>
        <div className="flex items-center gap-2 font-bold text-slate-900">{brand}</div>
        {statusBadge}
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button type="button" variant="outline" size="sm" aria-label="User menu">
            {userLabel}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {(userLinks ?? []).map((link) => (
            <DropdownMenuItem key={link.to} asChild>
              <Link to={link.to}>{link.label}</Link>
            </DropdownMenuItem>
          ))}
          <DropdownMenuItem onClick={onSignOut}>
            <LogOut className="mr-2 h-4 w-4" /> Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </header>
  );
};

export default SpaceTopbar;
