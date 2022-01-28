.PHONY: build cluster test

default: build

build:
	go build -o bin/swarm    github.com/MeshBoxFoundation/meshbox/cmd/swarm
	go build -o bin/geth     github.com/MeshBoxFoundation/meshbox/cmd/geth
	go build -o bin/bootnode github.com/MeshBoxFoundation/meshbox/cmd/bootnode

cluster: build
	scripts/boot-cluster.sh

test:
	go test -v github.com/MeshBoxFoundation/meshbox/swarm/...
