#!/bin/zsh
# LaunchAgent entry point: load web.env, then run the standalone server.
set -a; source $HOME/aiandtechnews/web.env; set +a
export HOSTNAME=127.0.0.1 PORT=3002 NODE_ENV=production
cd $HOME/aiandtechnews/web
exec $HOME/aiandtechnews/node/bin/node server.js
