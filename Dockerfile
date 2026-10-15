# muse-proxy-go 的多阶段构建样例。仅构建，不含任何密钥；运行期用只读根文件系统即可。
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/muse-proxy-go ./cmd/muse-proxy-go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/muse-proxy-go /muse-proxy-go
EXPOSE 19287
ENTRYPOINT ["/muse-proxy-go"]
