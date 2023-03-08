FROM alpine:latest AS builder
RUN apk update
RUN apk add curl
RUN curl -L --output redis-cell https://github.com/brandur/redis-cell/releases/download/v0.2.5/redis-cell-v0.2.5-x86_64-unknown-linux-gnu.tar.gz
RUN tar -zxf redis-cell

FROM redislabs/rejson:latest as rejson

FROM redislabs/redismod AS final
ENV LD_LIBRARY_PATH /usr/lib/redis/modules

COPY --from=builder --chown=redis:redis libredis_cell.so /usr/lib/redis/modules/libredis_cell.so
COPY --from=rejson ${LD_LIBRARY_PATH}/*.so ${LD_LIBRARY_PATH}/

RUN ls
RUN pwd
CMD ["--loadmodule", "/usr/lib/redis/modules/libredis_cell.so"]