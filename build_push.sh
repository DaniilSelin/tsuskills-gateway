#!/bin/bash
set -e
if [ -z "$1" ]; then echo "Usage: ./build_push.sh <version>"; exit 1; fi
VERSION=$1
IMAGE_NAME="daniilselin/tsu-skills-gateway"
echo "Building $IMAGE_NAME:$VERSION ..."
docker build -t $IMAGE_NAME:$VERSION -f docker/Dockerfile .
docker tag $IMAGE_NAME:$VERSION $IMAGE_NAME:latest
docker push $IMAGE_NAME:$VERSION
docker push $IMAGE_NAME:latest
echo "Done! Tags: $VERSION and latest"
