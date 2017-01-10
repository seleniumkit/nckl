FROM ubuntu:16.04
MAINTAINER Ivan Krutov <vania-pooh@yandex-team.ru>

ENV PORT 8080
ENV USERS_FILE /etc/grid-router/users.htpasswd
ENV QUOTA_DIRECTORY /etc/grid-router/quota
ENV DESTINATION localhost:4444
ENV ENDPOINTS http://127.0.0.1:2379

COPY nckl /usr/bin
COPY entrypoint.sh /

EXPOSE $PORT
ENTRYPOINT /entrypoint.sh
