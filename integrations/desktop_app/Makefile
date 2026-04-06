# Media Utopia Desktop App — convenience wrapper

BUILDDIR = builddir

.PHONY: setup build run clean install

setup:
	meson setup $(BUILDDIR)

build: setup
	meson compile -C $(BUILDDIR)

run: build
	@# Compile local GSettings schemas so the app can run uninstalled
	@mkdir -p $(BUILDDIR)/schemas
	@cp data/com.mediautopia.desktop.gschema.xml $(BUILDDIR)/schemas/
	@glib-compile-schemas $(BUILDDIR)/schemas/
	GSETTINGS_SCHEMA_DIR=$(BUILDDIR)/schemas ./$(BUILDDIR)/media-utopia

clean:
	rm -rf $(BUILDDIR)

install: build
	meson install -C $(BUILDDIR)
