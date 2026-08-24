# Newsletter setup

The newsletter uses double opt-in. An address becomes active only after the reader follows the confirmation link. Active readers receive a welcome email and the daily digest. Every welcome and digest email contains a signed unsubscribe link.

## 1. Configure Resend

1. Add and verify `aiandtech.news` in Resend.
2. Add the DNS records supplied by Resend.
3. Create a sending API key.
4. Use a sender such as `AI & Tech News <news@aiandtech.news>`.

The backend sends through `POST https://api.resend.com/emails`. No provider credentials are stored in the database or browser.

## 2. Configure the backend

Set the variables listed in `apps/server/.env.example` in the production backend environment.

- `RESEND_API_KEY` authorizes email delivery.
- `NEWSLETTER_FROM` must use a verified Resend domain.
- `NEWSLETTER_REPLY_TO` is optional.
- `NEWSLETTER_SITE_URL` must be `https://aiandtech.news` in production.
- `NEWSLETTER_TOKEN_SECRET` signs confirmation and unsubscribe links. Use at least 32 random characters and keep it stable.
- `NEWSLETTER_CRON_SECRET` protects the digest endpoint.

Restarting the backend runs the database migration. Existing addresses from the old unconfirmed form remain pending and do not receive a digest until they subscribe again and confirm.

## 3. Configure Vercel

Set `CRON_SECRET` on the production Vercel project to the exact same value as the backend's `NEWSLETTER_CRON_SECRET`. Keep the existing API URL configuration.

`apps/web/vercel.json` invokes `/api/newsletter/digest` once each day at `05:00 UTC`, which is `08:00` in Istanbul. Vercel may invoke daily cron jobs later within that hour on Hobby plans.

## 4. Verify production

1. Submit an address through both the header and inline forms.
2. Confirm that the address is stored with `status = pending`.
3. Open the confirmation email and follow the link.
4. Confirm that the address changes to `status = active` and the welcome email arrives.
5. Trigger the secured digest endpoint once and confirm one delivery row is stored for the current edition.
6. Trigger the same edition again and confirm it is skipped.
7. Follow the unsubscribe link and confirm the address changes to `status = unsubscribed`.

Never place `RESEND_API_KEY`, `NEWSLETTER_TOKEN_SECRET`, or either cron secret in a client-visible environment variable.
