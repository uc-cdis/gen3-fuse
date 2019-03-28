#!/bin/bash

git submodule init
git submodule update
go get -u golang.org/x/crypto/ssh/terminal
go get -u golang.org/x/sys/unix
go get -u golang.org/x/net/context
go get -u gopkg.in/yaml.v2
go get -u github.com/gorilla/mux
go build -o gen3fuse main.go

if [ $? -eq 0 ]; then
    printf "\nBuild succeeded. You can run the program like so: ./gen3fuse <path to config yaml file> <path to manifest json file> <directory to mount>\n"
else
    printf "\nBuild failed.\n"
    exit 1
fi