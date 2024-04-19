#!/bin/bash
set -e

go test -v

build() {
  builddir=build/${1}_${2}
  mkdir -p $builddir
  GOOS=$1 GOARCH=$2 GO386=softfloat go build -ldflags "-s -w" -o $builddir/quickfile
  echo "Compiled $builddir"

  distdir=dist
  mkdir -p $distdir
  zip $distdir/quickfile_${1}_${2}.zip $builddir/quickfile
}

build windows amd64
build windows 386
build linux amd64
build linux 386
build darwin amd64
build darwin arm64

echo "Done"

