# Cadott Programming Club website

The public site at https://cadottprogrammingclub.com: home page, students page, lesson slides and browser-playable club games. Go, templ, shadcn-templ and Tailwind.

## Develop

    task dev

## Add a lesson or game

Edit `content.go`. Lesson slides are copied from the Dropbox curriculum folder and games are exported from `~/projects/Cadott-Programming-Club/<slug>` by `sync-content.sh`.

## Deploy

    ./deploy.sh

Builds a Linux binary, syncs `content/` to `/var/www/cadottprogrammingclub` on jarvis, and restarts the `cadott-website` service (port 8095, behind nginx). `SKIP_CONTENT=1 ./deploy.sh` skips re-exporting games.
