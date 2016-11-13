FROM ubuntu:16.04
MAINTAINER Ivan Krutov <vania-pooh@yandex-team.ru>

ENV USERS_FILE /etc/grid-router/users.properties
ENV QUOTA_DIRECTORY /etc/grid-router/quota
ENV DESTINATION localhost:4444

CMD cd .. && go build && go test -race && cp -f ../nckl .
COPY nckl /usr/bin
COPY entrypoint.sh /

EXPOSE 8080
ENTRYPOINT /entrypoint.sh
