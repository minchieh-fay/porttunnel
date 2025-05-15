#!/bin/bash

set -e

MYPWD=$(cd `dirname $0`;pwd)

#获取一个和git版本相关的version
version=$(git rev-parse --short HEAD)

tagName="porttunnel.${version}"
imgName="hub.hitry.io/hitry/tools:${tagName}"


dbaa --platform=linux/amd64,linux/arm64 -t $imgName -f Dockerfile $MYPWD --push

echo "Build ok : $imgName"