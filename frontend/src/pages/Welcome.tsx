import { Building2, Check, HeartHandshake, ShieldCheck, Smartphone, Wallet } from "lucide-react";
import { Link } from "react-router-dom";

const networkRows = [
  { name: "M-Pesa", code: "MP", tone: "border-[#F4B942] bg-[#F4B942]/10 text-[#102A43]" },
  { name: "Airtel Money", code: "AM", tone: "border-[#E76F51] bg-[#E76F51]/10 text-[#102A43]" },
  { name: "Tigo Pesa", code: "TP", tone: "border-[#0B7285] bg-[#0B7285]/10 text-[#102A43]" },
];

const merchantFeatures = [
  "Business account and organization setup",
  "Apps, API keys, and webhook endpoints",
  "Sandbox testing before live payments",
];

const individualFeatures = [
  "Personal account with no business setup",
  "A support page for receiving payments or tips",
  "Identity verification for live payouts",
];

const Welcome = () => (
  <main className="min-h-screen overflow-hidden bg-[#F4EFE6] font-landing-body text-[#102A43]">
    <section className="bg-[#102A43] text-[#F4EFE6]">
      <div className="mx-auto max-w-7xl px-5 pb-16 pt-5 sm:px-8 sm:pb-24 lg:px-10">
        <header className="flex items-center justify-between border-b border-white/15 pb-5">
          <Link to="/" className="group inline-flex items-center gap-2.5 rounded-sm text-[#F4EFE6] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#F4B942] focus-visible:ring-offset-2 focus-visible:ring-offset-[#102A43]">
            <span className="grid h-9 w-9 place-items-center bg-[#F4B942] text-[#102A43] transition-transform group-hover:rotate-6">
              <Wallet className="h-5 w-5" strokeWidth={2.5} />
            </span>
            <span className="font-landing-display text-xl font-bold tracking-[-0.04em]">LipaGO</span>
          </Link>
          <nav className="flex items-center gap-4 text-xs font-semibold text-[#D9EEF0] sm:gap-7" aria-label="Main navigation">
            <a href="#paths" className="hidden transition-colors hover:text-[#F4B942] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#F4B942] sm:inline">Choose your path</a>
            <Link to="/admin/login" className="border-b border-[#F4B942] pb-1 text-[#F4B942] transition-colors hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#F4B942]">Operator access</Link>
          </nav>
        </header>

        <div className="grid gap-12 pt-14 lg:grid-cols-[minmax(0,0.9fr)_minmax(420px,1.1fr)] lg:items-center lg:gap-20 lg:pt-20">
          <div className="landing-entrance">
            <div className="mb-6 inline-flex items-center gap-2 border border-[#D9EEF0]/35 px-3 py-2 text-[0.68rem] font-bold uppercase tracking-[0.14em] text-[#D9EEF0]">
              <span className="h-2 w-2 bg-[#F4B942]" aria-hidden="true" />
              Payment infrastructure for East Africa
            </div>
            <h1 className="max-w-3xl font-landing-display text-[3.15rem] font-bold leading-[0.94] tracking-[-0.065em] text-[#F4EFE6] sm:text-6xl lg:text-[5.6rem]">
              Mobile money comes from many directions. Keep one clear ledger.
            </h1>
            <p className="mt-7 max-w-xl text-base leading-7 text-[#D9EEF0] sm:text-lg">
              LipaGO brings familiar mobile-money networks into one payment system, so your business or personal account can receive, track, and pay out without losing the thread.
            </p>
            <div className="mt-8 flex flex-col gap-3 sm:flex-row">
              <Link to="/merchant/register" className="inline-flex min-h-12 items-center justify-center bg-[#F4B942] px-5 text-sm font-bold text-[#102A43] transition-colors hover:bg-[#ffd36b] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#F4B942] focus-visible:ring-offset-2 focus-visible:ring-offset-[#102A43]">
                Create your business account
              </Link>
              <Link to="/creator/register" className="inline-flex min-h-12 items-center justify-center border border-[#D9EEF0]/55 px-5 text-sm font-bold text-[#F4EFE6] transition-colors hover:border-[#E76F51] hover:bg-[#E76F51] hover:text-[#102A43] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#E76F51] focus-visible:ring-offset-2 focus-visible:ring-offset-[#102A43]">
                Create your personal account
              </Link>
            </div>
            <p className="mt-6 text-xs text-[#D9EEF0]/75">
              Start in sandbox. Verify when you are ready for live payments.
            </p>
          </div>

          <div className="landing-entrance-delay relative" aria-label="Mobile-money networks converging into one ledger" role="img">
            <div className="relative border border-[#D9EEF0]/30 bg-[#174A43] p-4 sm:p-6">
              <div className="mb-7 flex items-center justify-between border-b border-[#D9EEF0]/20 pb-4">
                <div className="flex items-center gap-2 text-xs font-bold uppercase tracking-[0.16em] text-[#D9EEF0]">
                  <Smartphone className="h-4 w-4 text-[#F4B942]" />
                  Incoming rails
                </div>
                <span className="font-mono text-[0.65rem] text-[#D9EEF0]/60">LIVE / SANDBOX</span>
              </div>
              <div className="grid gap-4 sm:grid-cols-[1fr_72px_1.05fr] sm:items-center">
                <div className="space-y-3">
                  {networkRows.map((network) => (
                    <div key={network.name} className={`flex items-center gap-3 border px-3 py-3 text-sm font-bold ${network.tone}`}>
                      <span className="grid h-7 w-7 place-items-center border border-current font-mono text-[0.62rem]">{network.code}</span>
                      {network.name}
                    </div>
                  ))}
                </div>
                <div className="relative hidden h-40 sm:block" aria-hidden="true">
                  <span className="absolute left-0 top-[20%] h-px w-full bg-[#F4B942]" />
                  <span className="absolute left-0 top-1/2 h-px w-full bg-[#E76F51]" />
                  <span className="absolute bottom-[20%] left-0 h-px w-full bg-[#D9EEF0]" />
                  <span className="absolute right-0 top-1/2 h-3 w-3 -translate-y-1/2 rotate-45 border-r border-t border-[#F4B942]" />
                </div>
                <div className="flex items-center gap-3 border-2 border-[#F4B942] bg-[#F4EFE6] p-4 text-[#102A43] sm:block sm:p-5">
                  <div className="grid h-10 w-10 shrink-0 place-items-center bg-[#F4B942] sm:h-12 sm:w-12">
                    <Wallet className="h-6 w-6" />
                  </div>
                  <div className="mt-0 sm:mt-7">
                    <p className="font-landing-display text-2xl font-bold tracking-[-0.05em] sm:text-3xl">ONE</p>
                    <p className="text-xs font-bold uppercase tracking-[0.14em] text-[#0B7285]">clear ledger</p>
                  </div>
                </div>
              </div>
              <div className="mt-7 flex items-center justify-between border-t border-[#D9EEF0]/20 pt-4 font-mono text-[0.68rem] text-[#D9EEF0]/70">
                <span>orders reconciled</span>
                <span className="text-[#F4B942]">● ready</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section className="mx-auto max-w-7xl px-5 py-16 sm:px-8 sm:py-24 lg:px-10" id="paths">
      <div className="max-w-2xl">
        <p className="text-xs font-bold uppercase tracking-[0.14em] text-[#0B7285]">Two ways in</p>
        <h2 className="mt-3 font-landing-display text-4xl font-bold leading-none tracking-[-0.06em] sm:text-5xl">Built around the person receiving the money.</h2>
        <p className="mt-5 text-base leading-7 text-[#486581]">A business operation and a personal account should not feel like the same doorway. Choose the one that fits what you are trying to do.</p>
      </div>

      <div className="mt-12 grid gap-5 lg:grid-cols-[1.15fr_0.85fr]">
        <article className="relative overflow-hidden border-2 border-[#174A43] bg-[#174A43] p-6 text-[#F4EFE6] sm:p-9">
          <div className="absolute -right-8 -top-8 h-36 w-36 border-[18px] border-[#F4B942]/30" aria-hidden="true" />
          <div className="relative flex h-full flex-col">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="text-xs font-bold uppercase tracking-[0.14em] text-[#F4B942]">Business path</p>
                <h3 className="mt-3 font-landing-display text-3xl font-bold tracking-[-0.05em] sm:text-4xl">For the operation behind the sale.</h3>
              </div>
              <Building2 className="h-8 w-8 shrink-0 text-[#F4B942]" strokeWidth={1.5} />
            </div>
            <p className="mt-6 max-w-xl text-base leading-7 text-[#D9EEF0]">Set up your business, connect the apps that take payments, and see every movement in one place before you go live.</p>
            <ul className="mt-7 grid gap-3 border-t border-[#D9EEF0]/20 pt-6 sm:grid-cols-3">
              {merchantFeatures.map((feature) => <li key={feature} className="flex gap-2 text-sm leading-5 text-[#F4EFE6]"><Check className="mt-0.5 h-4 w-4 shrink-0 text-[#F4B942]" />{feature}</li>)}
            </ul>
            <div className="mt-8 flex flex-wrap items-center gap-5">
              <Link to="/merchant/register" className="inline-flex min-h-11 items-center justify-center bg-[#F4B942] px-5 text-sm font-bold text-[#102A43] transition-colors hover:bg-[#ffd36b] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#F4B942] focus-visible:ring-offset-2 focus-visible:ring-offset-[#174A43]">Create your business account</Link>
              <Link to="/merchant/login" className="text-sm font-bold text-[#D9EEF0] underline decoration-[#F4B942] decoration-2 underline-offset-4 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#F4B942]">Merchant sign in</Link>
            </div>
          </div>
        </article>

        <article className="relative overflow-hidden border border-[#E76F51] bg-[#fffaf2] p-6 sm:p-9">
          <div className="absolute bottom-0 right-0 h-28 w-28 border-l border-t border-[#E76F51]/40" aria-hidden="true" />
          <div className="relative flex h-full flex-col">
            <div className="flex items-start justify-between gap-4">
              <div>
                <p className="text-xs font-bold uppercase tracking-[0.14em] text-[#E76F51]">Personal path</p>
                <h3 className="mt-3 font-landing-display text-3xl font-bold tracking-[-0.05em] sm:text-4xl">For receiving money in your own name.</h3>
              </div>
              <HeartHandshake className="h-8 w-8 shrink-0 text-[#E76F51]" strokeWidth={1.5} />
            </div>
            <p className="mt-6 text-base leading-7 text-[#486581]">Make a personal account, share your page, and receive support, tips, or other payments without creating a business workspace.</p>
            <ul className="mt-7 grid gap-3 border-t border-[#E76F51]/20 pt-6">
              {individualFeatures.map((feature) => <li key={feature} className="flex gap-2 text-sm leading-5 text-[#102A43]"><Check className="mt-0.5 h-4 w-4 shrink-0 text-[#E76F51]" />{feature}</li>)}
            </ul>
            <div className="mt-8 flex flex-wrap items-center gap-5">
              <Link to="/creator/register" className="inline-flex min-h-11 items-center justify-center bg-[#E76F51] px-5 text-sm font-bold text-[#102A43] transition-colors hover:bg-[#f28b70] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#E76F51] focus-visible:ring-offset-2 focus-visible:ring-offset-[#fffaf2]">Create your personal account</Link>
              <Link to="/creator/login" className="text-sm font-bold text-[#102A43] underline decoration-[#E76F51] decoration-2 underline-offset-4 hover:text-[#E76F51] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#E76F51]">Individual sign in</Link>
            </div>
          </div>
        </article>
      </div>
    </section>

    <section className="border-y border-[#102A43]/15 bg-[#D9EEF0]">
      <div className="mx-auto flex max-w-7xl flex-col gap-6 px-5 py-8 sm:flex-row sm:items-center sm:justify-between sm:px-8 lg:px-10">
        <div className="flex items-start gap-4">
          <ShieldCheck className="mt-1 h-6 w-6 shrink-0 text-[#0B7285]" />
          <div>
            <p className="font-landing-display text-xl font-bold tracking-[-0.04em]">Behind the scenes, the rails stay accountable.</p>
            <p className="mt-1 max-w-2xl text-sm leading-6 text-[#486581]">LipaGO operators review KYC, oversee payments and withdrawals, manage providers, and keep the system reliable.</p>
          </div>
        </div>
        <Link to="/admin/login" className="shrink-0 self-start border-b-2 border-[#0B7285] pb-1 text-sm font-bold text-[#102A43] transition-colors hover:border-[#E76F51] hover:text-[#E76F51] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#0B7285] sm:self-auto">Operator sign in</Link>
      </div>
    </section>

    <footer className="mx-auto flex max-w-7xl flex-col gap-3 px-5 py-8 text-xs text-[#486581] sm:flex-row sm:items-center sm:justify-between sm:px-8 lg:px-10">
      <div className="flex items-center gap-2 font-bold text-[#102A43]"><Wallet className="h-4 w-4 text-[#0B7285]" /> LipaGO</div>
      <p>Sandbox first. Verify to unlock live payments.</p>
    </footer>
  </main>
);

export default Welcome;
