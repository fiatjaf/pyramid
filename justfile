dev:
    fd 'go|templ' | entr -r bash -c 'just templ && go build -o ./pyramid-exe && godotenv ./pyramid-exe'

build: templ
    CC=musl-gcc go build -ldflags='-linkmode external -extldflags "-static"' -o ./pyramid-exe

templ:
    templ generate

deploy target: build
    ssh root@{{target}} 'systemctl stop pyramid';
    scp pyramid-exe {{target}}:pyramid/pyramid
    ssh root@{{target}} 'systemctl start pyramid'
