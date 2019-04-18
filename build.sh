#!/bin/bash

go build -o gen3fuse main.go

if [ $? -eq 0 ]; then
    printf "\nBuild succeeded. You can run the program like so: ./gen3fuse <path to config yaml file> <path to manifest json file> <directory to mount>\n"
else
    printf "\nBuild failed.\n"
    exit 1
fi