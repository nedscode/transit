#!/bin/bash
set -e

EXTRAS="Mgoogle/api/annotations.proto=github.com/gogo/googleapis/google/api"

echo "Ensuring dependencies installed"
for i in \
    github.com/gogo/protobuf \
    github.com/gogo/googleapis \
    github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway \
	github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger \
	github.com/mwitkow/go-proto-validators/protoc-gen-govalidators \
	github.com/rakyll/statik \
; do
    if [ ! -d "$GOPATH/src/$i" ]; then
        echo " - $i not found, attempting to get it"
        go get $i
    fi
done

for i in proto protoc-gen-gogo gogoproto; do
    if [ ! -x "$GOPATH/bin/$i" ]; then
        echo " - $i binary not found, attempting to install it"
        go get "github.com/gogo/protobuf/$i"
    fi
done

for i in protoc-gen-grpc-gateway protoc-gen-swagger; do
    if [ ! -x "$GOPATH/bin/$i" ]; then
        echo " - $i binary not found, attempting to install it"
        go get "github.com/grpc-ecosystem/grpc-gateway/$i"
    fi
done

for i in protoc-gen-govalidators; do
    if [ ! -x "$GOPATH/bin/$i" ]; then
        echo " - $i binary not found, attempting to install it"
        go get "github.com/mwitkow/go-proto-validators/$i"
    fi
done

cd proto
echo "Building proto files"
protoc \
    -I . \
    -I $GOPATH/src/ \
    -I $GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/ \
    -I $GOPATH/src/github.com/gogo/googleapis/ \
    --gogo_out=plugins=grpc,$EXTRAS:$GOPATH/src/ \
    --grpc-gateway_out=$EXTRAS:$GOPATH/src/ \
    --swagger_out=../static/doc/ \
    --govalidators_out=gogoimport=true,$EXTRAS:$GOPATH/src/ \
    transit.proto

echo "Patching files"
# https://github.com/grpc-ecosystem/grpc-gateway/issues/229.
sed -i "s/empty.Empty/types.Empty/g" transit.pb.gw.go
sed -i "s/.Id/.ID/g" transit.pb.gw.go

cp ../static/doc/transit.swagger.json ../static/doc/transit.swagger.json.orig
patch -f -d ../static/doc <patch.diff
(cd ../static/doc; diff -u transit.swagger.json.orig transit.swagger.json || true) > patch.diff

cd ../admin
yarn build
cp -rf dist ../static/admin

echo "Building API docs"
cd ..
statik -m -f -src static/
