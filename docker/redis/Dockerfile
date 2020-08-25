FROM alpine:3.12 AS builder
RUN apk update
RUN apk add curl
RUN curl -L --output redis-cell https://github.com/brandur/redis-cell/releases/download/v0.2.5/redis-cell-v0.2.5-x86_64-unknown-linux-gnu.tar.gz
RUN tar -zxf redis-cell
FROM redis:5.0 AS final
COPY --from=builder libredis_cell.so /data/libredis_cell.so

RUN ls
RUN pwd
CMD [ "redis-server", "--loadmodule", "/data/libredis_cell.so" ]