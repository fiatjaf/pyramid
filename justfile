dev:
    fd 'go|templ' | entr -r bash -c 'just templ && go build && godotenv ./pyramid'

build: templ
    CC=musl-gcc go build -ldflags='-linkmode external -extldflags "-static"' -o ./pyramid

templ:
    templ generate

deploy target: build
    ssh root@{{target}} 'systemctl stop pyramid';
    scp pyramid {{target}}:pyramid/pyramid
    ssh root@{{target}} 'systemctl start pyramid'
