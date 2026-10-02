import { useState } from "react";
import { Check, Copy, MessageCircle, Share2 } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";

// Share-my-page panel for creators: copyable URL, client-rendered QR
// (no Block 3 QR service exists yet — pure client-side SVG, same value),
// and formatted share intents (no platform API integration).
const SharePanel = ({ handle, displayName }: { handle: string; displayName: string }) => {
  const [copied, setCopied] = useState<string | null>(null);
  const url = `${window.location.origin}/c/${handle}`;

  const copy = async (text: string, key: string, message: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      toast.success(message);
      window.setTimeout(() => setCopied((c) => (c === key ? null : c)), 2000);
    } catch {
      toast.error("Unable to copy — long-press to copy manually.");
    }
  };

  const shareText = `Support ${displayName} on LipaGO ${url}`;
  const intents = [
    {
      key: "whatsapp",
      label: "WhatsApp",
      href: `https://wa.me/?text=${encodeURIComponent(shareText)}`,
    },
    {
      key: "x",
      label: "X / Twitter",
      href: `https://twitter.com/intent/tweet?text=${encodeURIComponent(shareText)}`,
    },
  ];
  const bioLine = `${displayName} · Support me: ${url}`;

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="flex items-center gap-2 text-sm font-bold text-slate-800">
          <Share2 className="h-4 w-4" /> Share my page
        </h3>

        <div className="mt-3 flex flex-col items-center gap-4 sm:flex-row sm:items-start">
          <div className="rounded-lg border border-slate-200 bg-white p-3">
            <QRCodeSVG value={url} size={140} level="M" title={`QR code for ${url}`} />
          </div>
          <div className="w-full flex-1 space-y-3">
            <div>
              <label className="text-sm font-medium text-slate-700">Page URL</label>
              <div className="mt-1 flex gap-2">
                <Input value={url} readOnly className="font-mono text-xs" />
                <Button type="button" size="sm" variant="outline" onClick={() => copy(url, "url", "Page URL copied.")}>
                  {copied === "url" ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                </Button>
              </div>
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Instagram bio line</label>
              <div className="mt-1 flex gap-2">
                <Input value={bioLine} readOnly className="text-xs" />
                <Button type="button" size="sm" variant="outline" onClick={() => copy(bioLine, "bio", "Bio line copied.")}>
                  {copied === "bio" ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                </Button>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              {intents.map((s) => (
                <Button key={s.key} type="button" size="sm" variant="outline" asChild>
                  <a href={s.href} target="_blank" rel="noopener noreferrer">
                    <MessageCircle className="mr-1 h-3.5 w-3.5" /> Share on {s.label}
                  </a>
                </Button>
              ))}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default SharePanel;
