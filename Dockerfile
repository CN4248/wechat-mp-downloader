# 快速构建：不编译 Go，直接把预编译的静态二进制塞进 alpine。
# 构建前先执行 ./scripts/build.sh（或 ./deploy.sh，它会自动构建）。
# 首次构建约 5–15 秒（取决于镜像拉取速度），远快于 golang 多阶段构建。
ARG ALPINE_IMAGE=docker.m.daocloud.io/library/alpine:3.20
FROM ${ALPINE_IMAGE}

ARG ALPINE_MIRROR=https://mirrors.aliyun.com/alpine
RUN if [ -n "$ALPINE_MIRROR" ]; then \
      sed -i "s#https://dl-cdn.alpinelinux.org/alpine#${ALPINE_MIRROR}#g" /etc/apk/repositories; \
    fi \
 && apk add --no-cache ca-certificates tzdata \
 && rm -rf /var/cache/apk/*

ENV TZ=Asia/Shanghai \
    LANG=C.UTF-8 \
    WXMP_OUT=/download

WORKDIR /download
COPY build/wxmp-linux-amd64 /usr/local/bin/wxmp
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/wxmp /usr/local/bin/docker-entrypoint.sh \
 && mkdir -p /download

EXPOSE 2178

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:2178/api/health >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["--web", "-listen", "0.0.0.0:2178", "-o", "/download", "-no-browser"]
