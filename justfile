dev:
    fd 'go|templ' | entr -r bash -c 'just templ && just tailwind && go build -o ./pyramid-exe && godotenv ./pyramid-exe'

build: templ tailwind
    #!/bin/bash

    # set the global variable `currentVersion` to the latest git tag if we're in it, otherwise use the name of the latest tag + the first 8 characters of the current commit
    VERSION=$(git describe --tags --exact-match 2>/dev/null || echo "$(git describe --tags --abbrev=0)-$(git rev-parse --short=8 HEAD)")

    # build with musl for maximum compatibility everywhere
    CC=musl-gcc go build -ldflags="-X main.currentVersion=$VERSION -linkmode external -extldflags \"-static\"" -o ./pyramid-exe

templ:
    templ generate

tailwind:
    ./node_modules/.bin/tailwindcss -i base.css -o static/styles.css

deploy target: build
    ssh root@{{target}} 'systemctl stop pyramid';
    scp pyramid-exe {{target}}:pyramid/pyramid
    ssh root@{{target}} 'systemctl start pyramid'
