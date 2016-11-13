FROM ubuntu:16.04
MAINTAINER Ivan Krutov <vania-pooh@yandex-team.ru>

ENV USERS_FILE
ENV QUOTA_DIRECTORY
ENV DESTINATION

CMD cd .. && go build && go test -race && cp -f ../nckl .
COPY nckl /usr/bin
COPY entrypoint.sh /

EXPOSE 8080
ENTRYPOINT /entrypoint.sh
