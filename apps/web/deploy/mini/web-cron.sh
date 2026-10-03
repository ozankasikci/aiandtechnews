#!/bin/zsh
# Replaces the Vercel crons while the mini serves the site.
# Usage: web-cron.sh newsletter/digest | indexnow
set -a; source $HOME/aiandtechnews/web.env; set +a
curl -sS -m 300 -H "Authorization: Bearer $CRON_SECRET" "http://127.0.0.1:3002/api/$1"
echo " [$(date -u +%FT%TZ) $1]"
