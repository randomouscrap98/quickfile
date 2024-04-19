#!/bin/bash
set -e

basedir=`pwd`

go test -v

build() {
  builddir=$basedir/build/${1}_${2}
  mkdir -p "$builddir"
  cp index.html "$builddir"
  GOOS=$1 GOARCH=$2 GO386=softfloat go build -ldflags "-s -w" -o "$builddir/quickfile"
  echo "Compiled $builddir"

  distdir=$basedir/dist
  mkdir -p "$distdir"
  zip "$distdir/quickfile_${1}_${2}.zip" "$builddir/quickfile"
}

cd cmd
build windows amd64
build windows 386
build linux amd64
build linux 386
build linux arm64
build darwin amd64
build darwin arm64

echo "Done"

