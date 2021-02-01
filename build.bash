#!/usr/bin/env bash

VERSION=`git describe --tags --dirty`

package=$1
destination=$2
if [[ -z "$package" ]]; then
  echo "usage: $0 <package-name> <destination>"
  exit 1
fi
package_split=(${package//\// })
package_name=${package_split[-1]}

platforms=("linux/amd64" "linux/386" "darwin/amd64")

for platform in "${platforms[@]}"
do
    platform_split=(${platform//\// })
    GOOS=${platform_split[0]}
    GOARCH=${platform_split[1]}
    output_name=$package_name'-'$VERSION'-'$GOOS'-'$GOARCH
    if [ $GOOS = "windows" ]; then
        output_name+='.exe'
    fi  

    env GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 GO111MODULE=on \
        go build -mod=vendor -tags netgo,builtinassets -x \
                -ldflags="-X main.Version=$VERSION" \
                -o $destination/$output_name $package
    if [ $? -ne 0 ]; then
        echo 'An error has occurred! Aborting the script execution...'
        exit 1
    fi
done

cd $destination
sha256sum *$VERSION* > SHA256SUMS
