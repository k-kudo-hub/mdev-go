# 品質ガードレールの実行口(ADR-0003)。
# CI(.github/workflows/ci.yml)もこの Makefile 経由で同じコマンドを実行するため、
# ローカルと CI で必ず同一バージョンのツールが使われる。
#
# ツールは go.mod の tool ディレクティブではなく、バージョンを固定した
# `go run <module>@<version>` で実行する。理由は
# .claude/todo/20260808-guardrail-evidence.md の「ツールバージョン固定方法の検証」を参照。

GOLANGCI_LINT_VERSION := v2.12.2
GO_ARCH_LINT_VERSION  := v1.17.0
GO_TEST_COVERAGE_VERSION := v2.19.0

GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GO_ARCH_LINT  := go run github.com/fe3dback/go-arch-lint@$(GO_ARCH_LINT_VERSION)
GO_TEST_COVERAGE := go run github.com/vladopajic/go-test-coverage/v2@$(GO_TEST_COVERAGE_VERSION)

COVERAGE_PROFILE := cover.out
BIN := bin/mdev

.PHONY: help
help: ## このヘルプを表示する
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: fmt
fmt: ## gofmt で整形する
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## gofmt の差分がないことを検査する
	@out=`gofmt -l .`; \
	if [ -n "$$out" ]; then \
		echo "gofmt の差分があります:"; \
		echo "$$out"; \
		gofmt -d .; \
		exit 1; \
	fi
	@echo "gofmt: no diff"

.PHONY: lint
lint: ## golangci-lint を実行する
	$(GOLANGCI_LINT) run ./...

.PHONY: arch
arch: ## go-arch-lint で依存方向を検査する
	$(GO_ARCH_LINT) check

.PHONY: test
test: ## race 検出付きでテストを実行しカバレッジプロファイルを出力する
	go test -race -covermode=atomic -coverprofile=$(COVERAGE_PROFILE) ./...

.PHONY: cover
cover: test ## カバレッジ閾値を検査する
	$(GO_TEST_COVERAGE) --config=.testcoverage.yml

.PHONY: build
build: ## バイナリをビルドする
	go build -o $(BIN) ./cmd/mdev

.PHONY: check
check: fmt-check lint arch cover build ## CI と同じ検査を一括実行する

.PHONY: clean
clean: ## 生成物を削除する
	rm -rf bin $(COVERAGE_PROFILE)
