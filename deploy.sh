#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
[ "${SKIP_CONTENT:-0}" = 1 ] || ./sync-content.sh
go tool templ generate
tailwindcss -i ./assets/css/globals.css -o ./assets/css/output.css --minify
go tool shadcn-templ bundle
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o bin/site-linux .
rsync -az --delete --exclude setup.sh content/ jarvis:/var/www/cadottprogrammingclub/
rsync -az bin/site-linux jarvis:/home/apprunner/cadott-website/site.new
ssh jarvis 'mv /home/apprunner/cadott-website/site.new /home/apprunner/cadott-website/site && sudo systemctl restart cadott-website && sleep 1 && curl -fsS http://127.0.0.1:8095/healthz >/dev/null && echo healthy'
echo "deployed: https://cadottprogrammingclub.com/"
