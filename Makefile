BINARY := muse-proxy-go
GOFLAGS := -trimpath
LDFLAGS := -s -w
# 只测本仓库的 Go 包，避免误扫验收临时目录。
PKGS := ./cmd/... ./internal/... ./tools/...

.PHONY: all build test race vet fmt fmt-check check tools run docker clean

all: build

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/muse-proxy-go

test:
	go test $(PKGS) -count=1

race:
	go test $(PKGS) -count=1 -race

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "以下文件未格式化："; echo "$$out"; exit 1; fi

# 聚合门禁：格式 + 路由↔文档机检 + vet + 单测，退出码 0 才算过。
check: fmt-check
	node scripts/check-api-docs.mjs
	go vet ./...
	go test $(PKGS) -count=1

# 构建验收用的假上游与假出口代理（不参与生产部署）。
tools:
	CGO_ENABLED=0 go build -o tools/fakeupstream/fakeupstream ./tools/fakeupstream
	CGO_ENABLED=0 go build -o tools/fakeproxy/fakeproxy ./tools/fakeproxy

run: build
	./$(BINARY)

docker:
	docker build -t muse-proxy-go:latest .

clean:
	rm -f $(BINARY) tools/fakeupstream/fakeupstream tools/fakeproxy/fakeproxy
