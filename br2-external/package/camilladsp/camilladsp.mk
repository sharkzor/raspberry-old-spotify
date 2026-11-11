################################################################################
#
# camilladsp
#
################################################################################

CAMILLADSP_VERSION = 4.1.3
CAMILLADSP_SITE = $(call github,HEnquist,camilladsp,v$(CAMILLADSP_VERSION))
CAMILLADSP_LICENSE = GPL-3.0
CAMILLADSP_LICENSE_FILES = LICENSE_GLPv3.txt
CAMILLADSP_DEPENDENCIES = alsa-lib host-pkgconf

CAMILLADSP_CARGO_ENV = PKG_CONFIG_ALLOW_CROSS=1
CAMILLADSP_CARGO_BUILD_OPTS = --no-default-features
CAMILLADSP_CARGO_INSTALL_OPTS = $(CAMILLADSP_CARGO_BUILD_OPTS)

$(eval $(cargo-package))

# Upstream has no Cargo.lock; inject ours before Buildroot vendors the crates.
# dl-wrapper runs "<topdir>/support/download/<name>-post-process", so climb to
# / and point at our script by absolute path.
CAMILLADSP_DOWNLOAD_POST_PROCESS = ../../../../../../../..$(BR2_EXTERNAL_SPOTIFYPI_PATH)/package/camilladsp/cargo-lock
CAMILLADSP_DL_ENV += BR_TOPDIR=$(TOPDIR)
