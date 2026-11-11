################################################################################
#
# spotifypi
#
################################################################################

SPOTIFYPI_VERSION = 1.0
SPOTIFYPI_SITE = $(BR2_EXTERNAL_SPOTIFYPI_PATH)/../webui
SPOTIFYPI_SITE_METHOD = local
SPOTIFYPI_LICENSE = MIT
SPOTIFYPI_GOMOD = spotifypi
SPOTIFYPI_LDFLAGS = -s -w -X main.version=$(SPOTIFYPI_VERSION)

define SPOTIFYPI_INSTALL_EVENT_LINK
	ln -sf spotifypi $(TARGET_DIR)/usr/bin/spotifypi-event
endef
SPOTIFYPI_POST_INSTALL_TARGET_HOOKS += SPOTIFYPI_INSTALL_EVENT_LINK

$(eval $(golang-package))
