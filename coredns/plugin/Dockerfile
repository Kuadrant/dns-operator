FROM alpine:3.21.0
COPY coredns /
COPY GeoLite2-City-demo.mmdb /
EXPOSE 53 53/udp
ENTRYPOINT ["/coredns"]
