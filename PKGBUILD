# Maintainer: Andrea Fiori <andrea.fiori.1998@gmail.com>
pkgname=aawk-git
_pkgname=aawk
pkgver=r173.71325f1
pkgrel=1
pkgdesc="POSIX-compliant AWK interpreter written in go"
arch=('any')
url="https://github.com/fioriandrea/aawk"
license=('GPL')
makedepends=(git go)
source=(git+https://github.com/fioriandrea/aawk)
md5sums=('SKIP')

pkgver() {
    cd "$_pkgname"
    printf "r%s.%s" "$(git rev-list --count HEAD)" "$(git rev-parse --short HEAD)"
}

build() {
    cd "$_pkgname"
    go build
}

package() {
    cd "$_pkgname"
    mkdir -p "$pkgdir/usr/bin"
    cp ./"$_pkgname" "$pkgdir/usr/bin/$_pkgname"
}
