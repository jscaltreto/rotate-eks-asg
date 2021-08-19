ifdef DRYRUN
	DRYRUN_FLAG=--dryrun
endif

.PHONY: deps
deps:
	GO111MODULE=on go build ./cmd/...
	GO111MODULE=on go mod tidy
	GO111MODULE=on go mod vendor

.PHONY: build
build:
	GO111MODULE=on GOBIN=${PWD}/bin go install -mod=vendor ./cmd/...

.PHONY: rotate
rotate: build
	${PWD}/bin/rotate-eks-asg ${ARGS} ${DRYRUN_FLAG}

.PHONY: rotate-oldest
rotate-oldest:
	${MAKE} -e ARGS="--limit=1" rotate

