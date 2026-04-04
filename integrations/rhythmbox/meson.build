project('mu-rhythmbox', ['vala', 'c'],
    version: '0.1.0',
    meson_version: '>= 0.56',
)

gnome = import('gnome')

glib_dep = dependency('glib-2.0', version: '>= 2.56')
gobject_dep = dependency('gobject-2.0')
gio_dep = dependency('gio-2.0')
gtk3_dep = dependency('gtk+-3.0', version: '>= 3.20')
json_glib_dep = dependency('json-glib-1.0')
peas_dep = dependency('libpeas-1.0')
peas_gtk_dep = dependency('libpeas-gtk-1.0')

# C-only dependencies: Rhythmbox headers transitively need libxml2, tdb, and
# gstreamer. We use the C compiler to find these so they DON'T get passed to
# valac as --pkg (we supply our own Vala bindings in vapi/).
cc = meson.get_compiler('c')
mosquitto_lib = cc.find_library('mosquitto')

# Transitive header deps required by rhythmbox's C headers.
libxml2_c_dep = dependency('libxml-2.0', native: false)
tdb_c_dep = dependency('tdb', native: false)
gstreamer_c_dep = dependency('gstreamer-1.0', native: false)

sources = files(
    'src/mu-protocol.vala',
    'src/mu-dedup.vala',
    'src/mqtt-client.vala',
    'src/mu-config.vala',
    'src/node-registry.vala',
    'src/mu-renderer-lease.vala',
    'src/mu-renderer-queue.vala',
    'src/mu-renderer.vala',
    'src/mu-entry-type.vala',
    'src/mu-library-source.vala',
    'src/mu-controller.vala',
    'src/mu-controller-panel.vala',
    'src/mu-plugin.vala',
)

rb_plugin_dir = get_option('libdir') / 'rhythmbox' / 'plugins' / 'mu'

# Rhythmbox C headers transitively include libxml2, tdb, gstreamer, etc.
# We add the include dirs that aren't already provided by our Vala-aware deps.
rb_includes = include_directories(
    '/usr/include/rhythmbox',
    '/usr/include/libxml2',
    '/usr/include/gstreamer-1.0',
    '/usr/include/libsoup-3.0',
    '/usr/include/totem-pl-parser/1/plparser',
)

shared_library('mu',
    sources,
    dependencies: [
        glib_dep,
        gobject_dep,
        gio_dep,
        gtk3_dep,
        json_glib_dep,
        peas_dep,
        peas_gtk_dep,
        mosquitto_lib,
    ],
    include_directories: rb_includes,
    # Rhythmbox symbols are resolved at runtime when the plugin is loaded into
    # the Rhythmbox process. Allow undefined references during linking.
    link_args: ['-Wl,--unresolved-symbols=ignore-all'],
    vala_args: [
        '--vapidir', meson.current_source_dir() / 'vapi',
        '--pkg', 'mosquitto',
        '--pkg', 'rb-3.0',
        '--pkg', 'gst-query',
    ],
    install: true,
    install_dir: rb_plugin_dir,
)

install_data('data/mu.plugin',
    install_dir: rb_plugin_dir,
)

install_data(
    'data/org.gnome.rhythmbox.plugins.mu.gschema.xml',
    install_dir: get_option('datadir') / 'glib-2.0' / 'schemas',
)
