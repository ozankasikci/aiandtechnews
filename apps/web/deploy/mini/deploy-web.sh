#!/bin/zsh
# Build the public site on the Mac mini and (re)start it as the standby
# fallback for Vercel: Next standalone on 127.0.0.1:3002, tunnelled to
# https://aitn-web.subtunnel.dev. Usage: deploy-web.sh <checkout on the SSD>
set -euo pipefail

CHECKOUT=${1:?usage: deploy-web.sh <checkout>}
W=/Volumes/Samsung990PRO/AIAndTechNews
RT=$HOME/aiandtechnews
LIVE=$RT/web
UID_=$(id -u)

source $W/env.sh
set -a; source $RT/web.env; set +a

cd $CHECKOUT/apps/web
pnpm install --frozen-lockfile
NEXT_STANDALONE=1 pnpm build

# The standalone server needs static assets and public/ next to it.
NEW=$RT/web.new
rm -rf $NEW
cp -R .next/standalone $NEW
mkdir -p $NEW/.next
cp -R .next/static $NEW/.next/static
cp -R public $NEW/public
git -C $CHECKOUT rev-parse HEAD > $NEW/COMMIT

rm -rf $RT/web.prev
[[ -d $LIVE ]] && mv $LIVE $RT/web.prev
mv $NEW $LIVE

if launchctl print gui/$UID_/news.aiandtech.web >/dev/null 2>&1; then
  launchctl kickstart -k gui/$UID_/news.aiandtech.web
else
  launchctl bootstrap gui/$UID_ $HOME/Library/LaunchAgents/news.aiandtech.web.plist
fi

for i in {1..30}; do
  code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:3002/ || true)
  [[ $code == 200 ]] && { echo "web up on :3002 ($(cat $LIVE/COMMIT))"; exit 0; }
  sleep 2
done
echo "web did not answer 200 on :3002; last code $code; see $RT/logs/web.err.log" >&2
exit 1
