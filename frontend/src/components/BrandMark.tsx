import { cn } from "@/lib/utils";

type BrandMarkProps = {
  className?: string;
};

export default function BrandMark({ className }: BrandMarkProps) {
  return <img src="/lipago-icon.png" alt="" aria-hidden="true" className={cn("shrink-0 object-contain", className)} />;
}
