GO ?= go
VERSION ?= dev

.PHONY: build test integration live check cross release-dry-run fmt clean

# 构建知识只存在于 scripts/ 下。Makefile 不重复架构列表、ldflags 或打包策略：
# 抄一份在这里，就等于多一个会和发布路径分叉的地方。

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/ecs ./cmd/ecs

test:
	$(GO) test ./...

integration:
	./scripts/ci/integration.sh

live:
	$(GO) test -tags=live ./... -v -timeout 20m

check:
	./scripts/ci/check.sh

cross:
	./scripts/cross.sh

# 演练完整发布链，跳过需要容器的工具构建。参数不在这里拼：--dry-run 的含义
# 由 verify.sh 自己定义，Makefile 只负责把三步串起来。
release-dry-run:
	./scripts/release/build.sh $(VERSION) --dry-run
	./scripts/release/verify.sh --dist dist --dry-run
	./scripts/release/security.sh --dist dist

fmt:
	$(GO) fmt ./...

clean:
	rm -rf bin dist .devtools-bin
