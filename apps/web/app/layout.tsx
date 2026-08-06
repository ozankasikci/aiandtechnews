import type { Metadata } from "next";
import Script from "next/script";
import "./globals.css";
import { Header } from "./components/Header";
import { Footer } from "./components/Footer";
import { Analytics } from "./components/Analytics";

const GA_ID = "G-32SP4ZKM67";

export const metadata: Metadata = {
  title: {
    default: "AI and Tech News, Daily | aiandtech.news",
    template: "%s | AI and Tech News",
  },
  description: "Breaking AI and technology news, daily. Covering artificial intelligence, startups, big tech, and developer tools.",
  openGraph: {
    siteName: "AI and Tech News",
    type: "website",
    url: "https://www.aiandtech.news",
  },
  metadataBase: new URL("https://www.aiandtech.news"),
  icons: {
    icon: [
      { url: "/favicon-16.png", sizes: "16x16", type: "image/png" },
      { url: "/favicon-32.png", sizes: "32x32", type: "image/png" },
      { url: "/favicon.ico" },
    ],
    apple: [{ url: "/apple-touch-icon.png", sizes: "180x180" }],
  },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800;900&display=swap" rel="stylesheet" />
      </head>
      <body>
        <Script src={`https://www.googletagmanager.com/gtag/js?id=${GA_ID}`} strategy="afterInteractive" />
        <Script id="ga-init" strategy="afterInteractive">
          {`window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());gtag('config','${GA_ID}',{send_page_view:false});`}
        </Script>
        <div className="min-h-screen bg-bg flex flex-col">
          <Header />
          <Analytics />
          <div className="flex-1">{children}</div>
          <Footer />
        </div>
      </body>
    </html>
  );
}
