FROM ubuntu:16.04
MAINTAINER Ivan Krutov <vania-pooh@yandex-team.ru>

CMD cd .. && go build && go test -race
COPY ../nckl /usr/bin
COPY entrypoint.sh /

ENTRYPOINT /entrypoint.sh
