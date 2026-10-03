# Mac mini fallback for www.aiandtech.news

Standby copy of the public site, for when Vercel pauses the Hobby project
(usage limits). It runs all the time on the mini and is reachable at
https://aitn-web.subtunnel.dev; switching the domain to it is a DNS change.

## Pieces on the mini

| What | Where |
|---|---|
| Site (Next standalone) | `~/aiandtechnews/web`, 127.0.0.1:3002, LaunchAgent `news.aiandtech.web` (`bin/run-web.sh`) |
| Env | `~/aiandtechnews/web.env` (0600): `NEXT_PUBLIC_API_URL`, `API_URL=http://127.0.0.1:4001`, `CRON_SECRET` (same as Vercel) |
| Tunnel | `~/aiandtechnews/web-subtunnel.toml`, subdomain `aitn-web`, LaunchAgent `news.aiandtech.web-tunnel` (separate from the API tunnel) |
| Crons | `news.aiandtech.cron-newsletter` (06:00 local), `news.aiandtech.cron-indexnow` (07:00 local): plists in `~/Library/LaunchAgents`, NOT loaded while Vercel serves the site, or the newsletter goes out twice |
| Logs | `~/aiandtechnews/logs/web*.log`, `cron-*.log` |

Update the standby after web changes (build on the SSD, swap, restart):

    ~/aiandtechnews/deploy-web.sh /Volumes/Samsung990PRO/AIAndTechNews/worktrees/<name>

## Switching to the mini (Vercel paused)

1. VPS: `nginx-aiandtech.conf` is installed and has a certificate (below).
2. name.com DNS for aiandtech.news:
   - `A  @    34.255.144.232` (replaces Vercel's 216.198.79.1)
   - `CNAME www tunnel.subtunnel.dev.` (replaces the vercel-dns CNAME)
3. On the mini, start the crons:

       launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/news.aiandtech.cron-newsletter.plist
       launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/news.aiandtech.cron-indexnow.plist

4. Check: `curl -sI https://www.aiandtech.news/ | head -3`.

The Go API's `SITE_REVALIDATE_URL` already targets https://www.aiandtech.news,
so revalidation follows the DNS change by itself.

## Switching back to Vercel

Restore the two DNS records (`A @ 216.198.79.1`, `CNAME www
cc406ffff4ff2aae.vercel-dns-017.com.`) and boot out the crons:

    launchctl bootout gui/$(id -u)/news.aiandtech.cron-newsletter
    launchctl bootout gui/$(id -u)/news.aiandtech.cron-indexnow

## TLS certificate

Either issue it ahead of time with a DNS challenge (TXT record at name.com,
valid 90 days, no downtime at switch):

    sudo certbot certonly --manual --preferred-challenges dns -d aiandtech.news -d www.aiandtech.news

or after the DNS switch with HTTP (a few minutes of certificate errors):

    sudo certbot certonly --webroot -w /var/www/html -d aiandtech.news -d www.aiandtech.news
