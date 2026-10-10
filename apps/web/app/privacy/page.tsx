import type { Metadata } from "next";
import Link from "next/link";

const CONTACT_URL = "https://github.com/ozankasikci/aiandtechnews/issues/new";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description: "What AI and Tech News collects when you read the site, sign up for the newsletter or see ads, and the choices you have.",
  alternates: { canonical: "/privacy" },
};

function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section id={id} className="mb-12 scroll-mt-24">
      <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted mb-6 pb-2 border-b border-border">{title}</h2>
      <div className="bg-bg-card rounded-sm p-6 space-y-4 text-text-secondary leading-relaxed">{children}</div>
    </section>
  );
}

const external = "text-accent-purple hover:text-white font-semibold transition-colors";

export default function PrivacyPage() {
  return (
    <div className="max-w-[800px] mx-auto px-4 lg:px-8 py-12">
      <h1 className="text-4xl md:text-5xl font-black tracking-tight mb-6">Privacy Policy</h1>

      <div className="border-b border-border pb-8 mb-10 space-y-4">
        <p className="text-text-secondary text-lg leading-relaxed">
          This page explains what AI and Tech News (www.aiandtech.news) collects when you read the site, sign up
          for the newsletter or see ads, and the choices you have.
        </p>
        <p className="text-text-muted text-sm">Last updated: October 11, 2026</p>
      </div>

      <Section id="reading" title="Reading the site">
        <p>You can read every article without an account. We do not ask for your name or sign you in.</p>
        <p>
          When you open an article, your browser tells our server that the article was viewed so we can count
          readers. To avoid counting the same view twice, the server keeps a short-lived scrambled fingerprint of
          the request for about 30 minutes. It does not store your IP address.
        </p>
        <p>
          Your browser also keeps a few things for you in its own local storage: the articles you opened recently
          (so we can suggest ones you have not read) and your daily quiz results and streak. This stays on your
          device and is not sent to us. Clearing your browser&apos;s site data removes it.
        </p>
      </Section>

      <Section id="analytics" title="Analytics">
        <p>
          We use Google Analytics to understand which pages are read and how readers find the site. Google
          Analytics sets cookies and collects information such as the pages you visit, your approximate location
          derived from your IP address, and your browser and device type. We remove search terms from the page
          addresses we send it.
        </p>
        <p>
          You can stop Google Analytics from collecting your visits with the{" "}
          <a href="https://tools.google.com/dlpage/gaoptout" target="_blank" rel="noreferrer" className={external}>
            Google Analytics opt-out add-on
          </a>
          .
        </p>
      </Section>

      <Section id="advertising" title="Advertising">
        <p>
          The site shows ads through Google AdSense. Google and its partners use cookies and similar technologies
          to serve ads, including ads based on your earlier visits to this and other websites, and to measure how
          ads perform.
        </p>
        <p>
          If you are in the European Economic Area, the United Kingdom or Switzerland, you are asked for your
          consent before personalised ads are shown, and you can change that choice at any time from the consent
          message on the site.
        </p>
        <p>
          You can turn off personalised advertising in{" "}
          <a href="https://adssettings.google.com" target="_blank" rel="noreferrer" className={external}>
            Google&apos;s Ads Settings
          </a>{" "}
          and read{" "}
          <a href="https://policies.google.com/technologies/partner-sites" target="_blank" rel="noreferrer" className={external}>
            how Google uses information from sites that use its services
          </a>
          . You can also opt out of personalised ads from many other companies at{" "}
          <a href="https://www.aboutads.info/choices/" target="_blank" rel="noreferrer" className={external}>
            aboutads.info
          </a>
          .
        </p>
      </Section>

      <Section id="newsletter" title="Newsletter">
        <p>
          If you sign up for the newsletter, we store your email address and whether you confirmed it, and we use
          it only to send you the daily digest. Emails are delivered through Resend, our email provider.
        </p>
        <p>
          Every email has an unsubscribe link. Once you unsubscribe, we stop sending you email.
        </p>
      </Section>

      <Section id="sharing" title="Who we share data with">
        <p>
          We do not sell personal information. The only companies that receive data from your visit are the
          service providers named above: Google (analytics and advertising) and Resend (newsletter email).
          Featured images are loaded from Amazon Web Services, which receives your IP address as part of
          delivering them.
        </p>
      </Section>

      <Section id="contact" title="Questions and requests">
        <p>
          To ask a question about this policy, or to ask us to delete your newsletter email address, contact us
          through the public project issue tracker. Please do not post personal information there beyond what is
          needed to handle the request.
        </p>
        <Link href={CONTACT_URL} target="_blank" rel="noreferrer" className={`inline-flex ${external}`}>
          Contact us
        </Link>
        <p>We will update this page when what we collect changes, and change the date at the top.</p>
      </Section>
    </div>
  );
}
