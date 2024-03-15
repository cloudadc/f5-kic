all: upload

dstamp := $(shell date +%Y%m%d)
tstamp := $(shell date +%H%M%S)
timestamp ?= ${dstamp}-${tstamp}
version ?= latest
docker_repo ?= cloudadc/k8s-bigip-ctlr-c

image_name := ${docker_repo}:${version}-${arch}-${timestamp}

upload: image
	docker push ${image_name}

image: binary
	cd .. && docker buildx build \
		--no-cache \
		-t ${image_name} \
		-f build/Dockerfile-${arch} .

binary:
ifndef arch
	$(error 'arch' not defined, i.e, arch=arm64 arch=amd64)
endif
	cd ../f5kic; \
	CGO_ENABLED=0 GOOS=linux GOARCH=${arch} \
	go build -ldflags '-s -w --extldflags "-static -fpic"' -o f5-kic-linux; \
	# CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
	# go build -ldflags '-s -w --extldflags "-static -fpic"' -o f5-kic-darwin

darwin:
	cd ../f5kic; \
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags '-s -w --extldflags "-static -fpic"' -o f5-kic-darwin

as3-parser:
ifndef arch
	# $(error 'arch' not defined, i.e, arch=arm64 arch=amd64)
endif
	cd ../as3-parser; \
	docker buildx build -t cloudadc/as3-parser:${version}-${arch}-${timestamp} -f Dockerfile-${arch} .
	docker tag cloudadc/as3-parser:${version}-${arch}-${timestamp} f5devcentral/cis-c-as3-parser:latest-${arch}-v${dstamp}

clean:
	rm ./f5kic/f5-kic-linux
	rm ./f5kic/f5-kic-darwin
	rm -fr ./as3-parser/f5-appsvcs*
