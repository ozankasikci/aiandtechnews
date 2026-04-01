"use client";

import { useState } from "react";

export function NewsletterBanner() {
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [message, setMessage] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email) return;
    setStatus("loading");
    try {
      const res = await fetch("/api/subscribe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      const data = await res.json();
      if (data.success) {
        setStatus("success");
        setMessage(data.message || "You're subscribed!");
        setEmail("");
      } else {
        setStatus("error");
        setMessage(data.error || "Something went wrong");
      }
    } catch {
      setStatus("error");
      setMessage("Something went wrong. Try again.");
    }
  };

  return (
    <div className="bg-bg-card border border-border rounded-sm p-6 my-8">
      <p className="text-[10px] font-bold uppercase tracking-widest text-text-muted mb-2">Newsletter</p>
      <h3 className="text-xl font-black mb-1">Get the best AI & tech news daily</h3>
      <p className="text-text-secondary text-sm mb-4">No spam. Unsubscribe anytime.</p>
      {status === "success" ? (
        <p className="text-accent-green font-semibold text-sm">{message}</p>
      ) : (
        <form onSubmit={submit} className="flex gap-2">
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="your@email.com"
            required
            className="flex-1 bg-bg border border-border rounded-sm px-4 py-2.5 text-sm text-white placeholder:text-text-muted focus:outline-none focus:border-accent-purple transition-colors"
          />
          <button
            type="submit"
            disabled={status === "loading"}
            className="bg-accent-purple hover:brightness-110 transition-all text-white text-sm font-bold px-5 py-2.5 rounded-sm shrink-0 disabled:opacity-50"
          >
            {status === "loading" ? "..." : "Subscribe"}
          </button>
        </form>
      )}
      {status === "error" && <p className="text-red-400 text-xs mt-2">{message}</p>}
    </div>
  );
}

export function SubscribeButton() {
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [message, setMessage] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email) return;
    setStatus("loading");
    try {
      const res = await fetch("/api/subscribe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      const data = await res.json();
      if (data.success) {
        setStatus("success");
        setMessage(data.message || "You're subscribed!");
        setEmail("");
      } else {
        setStatus("error");
        setMessage(data.error || "Something went wrong");
      }
    } catch {
      setStatus("error");
      setMessage("Something went wrong.");
    }
  };

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="text-text-secondary hover:text-white text-sm font-medium transition-colors"
      >
        Subscribe
      </button>

      {open && (
        <div className="fixed inset-0 z-[100] flex items-center justify-center p-4" onClick={() => setOpen(false)}>
          <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
          <div
            className="relative bg-bg-card border border-border rounded-sm p-8 max-w-md w-full shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <button
              onClick={() => setOpen(false)}
              className="absolute top-4 right-4 text-text-muted hover:text-white transition-colors"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
            <p className="text-[10px] font-bold uppercase tracking-widest text-accent-purple mb-3">Newsletter</p>
            <h2 className="text-2xl font-black mb-2">Stay in the loop</h2>
            <p className="text-text-secondary text-sm mb-6">The best AI & tech stories, delivered daily. No spam. Unsubscribe anytime.</p>
            {status === "success" ? (
              <div className="text-center py-4">
                <div className="text-4xl mb-3">✓</div>
                <p className="text-accent-green font-bold text-lg">{message}</p>
                <p className="text-text-secondary text-sm mt-1">Welcome to the list.</p>
              </div>
            ) : (
              <form onSubmit={submit} className="flex flex-col gap-3">
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="your@email.com"
                  required
                  autoFocus
                  className="bg-bg border border-border rounded-sm px-4 py-3 text-white placeholder:text-text-muted focus:outline-none focus:border-accent-purple transition-colors"
                />
                <button
                  type="submit"
                  disabled={status === "loading"}
                  className="bg-accent-purple hover:brightness-110 transition-all text-white font-bold py-3 rounded-sm disabled:opacity-50"
                >
                  {status === "loading" ? "Subscribing..." : "Subscribe"}
                </button>
                {status === "error" && <p className="text-red-400 text-xs">{message}</p>}
              </form>
            )}
          </div>
        </div>
      )}
    </>
  );
}
