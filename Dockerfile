FROM alpine:3.15
ENTRYPOINT ["/grazer"]
STOPSIGNAL SIGINT
COPY grazer /grazer
