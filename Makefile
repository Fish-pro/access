# Image URL to use all building/pushing image targets
IMG ?= fishpro3/access:latest

.PHONY: manifests
manifests:
	hack/update-codegen.sh

# It is only available on ubuntu machines
.PHONY: generate
generate:
	apt update && apt install clang llvm && export BPF_CLANG=clang
	cd bpf/blips && rm bpf_*.go *.o && go generate
	hack/update-crdgen.sh

.PHONY: docker-build
docker-build:
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push:
	docker push ${IMG}
