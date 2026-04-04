BUILDDIR := builddir
PLUGIN_DIR := $(HOME)/.local/share/rhythmbox/plugins/mu
SCHEMA_DIR := $(HOME)/.local/share/glib-2.0/schemas

.PHONY: all setup build install uninstall clean dev-install

all: build

setup:
	meson setup $(BUILDDIR)

build: setup
	meson compile -C $(BUILDDIR)

# Install to system paths (requires sudo).
install: build
	meson install -C $(BUILDDIR)
	glib-compile-schemas $(shell pkg-config --variable=prefix glib-2.0)/share/glib-2.0/schemas/ 2>/dev/null || true

# Install to user-local paths (no sudo needed).
dev-install: build
	@mkdir -p $(PLUGIN_DIR) $(SCHEMA_DIR)
	cp $(BUILDDIR)/libmu.so $(PLUGIN_DIR)/libmu.so
	cp data/mu.plugin $(PLUGIN_DIR)/mu.plugin
	cp data/org.gnome.rhythmbox.plugins.mu.gschema.xml $(SCHEMA_DIR)/
	glib-compile-schemas $(SCHEMA_DIR)
	@echo "Installed to $(PLUGIN_DIR)"
	@echo "Restart Rhythmbox, then enable 'Media Utopia' in Edit > Plugins"

uninstall:
	rm -rf $(PLUGIN_DIR)
	rm -f $(SCHEMA_DIR)/org.gnome.rhythmbox.plugins.mu.gschema.xml
	glib-compile-schemas $(SCHEMA_DIR) 2>/dev/null || true

clean:
	rm -rf $(BUILDDIR)
