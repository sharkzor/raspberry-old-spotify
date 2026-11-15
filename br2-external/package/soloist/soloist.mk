################################################################################
#
# soloist
#
################################################################################

# Spotify ships a rolling "latest" build at a fixed URL, so there is no
# version pin and no .hash file. The build script always re-downloads it.
SOLOIST_VERSION = latest
SOLOIST_SITE = https://soloist-builds.spotifycdn.com
SOLOIST_SOURCE = soloist_release_arm32.tar.gz
# The archive has no top-level directory: its files sit at the root.
SOLOIST_STRIP_COMPONENTS = 0
SOLOIST_LICENSE = Proprietary (Spotify Soloist); third-party licenses in THIRD_PARTY_LICENSES.txt
SOLOIST_LICENSE_FILES = THIRD_PARTY_LICENSES.txt
SOLOIST_REDISTRIBUTE = NO
SOLOIST_DEPENDENCIES = pulseaudio

define SOLOIST_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/soloist $(TARGET_DIR)/usr/bin/soloist
endef

$(eval $(generic-package))
