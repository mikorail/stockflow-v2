#!/bin/sh
for d in "$HOME"/.nvm/versions/node/v24*/bin; do [ -d "$d" ] && PATH="$d:$PATH"; done
exec npm run dev
