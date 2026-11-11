################################################################################
#
# librespot
#
################################################################################

LIBRESPOT_VERSION = 0.8.0
LIBRESPOT_SITE = $(call github,librespot-org,librespot,v$(LIBRESPOT_VERSION))
LIBRESPOT_LICENSE = MIT
LIBRESPOT_LICENSE_FILES = LICENSE
LIBRESPOT_DEPENDENCIES = alsa-lib host-pkgconf

LIBRESPOT_CARGO_ENV = PKG_CONFIG_ALLOW_CROSS=1
LIBRESPOT_CARGO_BUILD_OPTS = \
	--no-default-features \
	--features alsa-backend,with-libmdns,rustls-tls-webpki-roots
LIBRESPOT_CARGO_INSTALL_OPTS = $(LIBRESPOT_CARGO_BUILD_OPTS)

$(eval $(cargo-package))
