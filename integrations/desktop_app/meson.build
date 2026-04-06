project(
    'media-utopia',
    ['vala', 'c'],
    version: '0.1.0',
    meson_version: '>= 0.62.0',
    default_options: [
        'warning_level=1',
    ],
)

gnome = import('gnome')

# Dependencies
gtk4_dep = dependency('gtk4', version: '>= 4.12')
adw_dep = dependency('libadwaita-1', version: '>= 1.4')
gstreamer_dep = dependency('gstreamer-1.0', version: '>= 1.22')
gstreamer_audio_dep = dependency('gstreamer-audio-1.0', version: '>= 1.22')
json_glib_dep = dependency('json-glib-1.0', version: '>= 1.8')
libsoup_dep = dependency('libsoup-3.0', version: '>= 3.4')
# libmosquitto — C linkage only; Vala bindings come from vapi/mosquitto.vapi
cc = meson.get_compiler('c')
mosquitto_c_dep = cc.find_library('mosquitto', required: true)

# Optional: tray icon support (C-only — system VAPI is GTK3, not GTK4-compatible)
ayatana_dep = dependency(
    'ayatana-appindicator3-0.1',
    required: false,
)
# Note: ayatana is detected but NOT linked until we have a GTK4-compatible VAPI.
# The HAVE_AYATANA flag is set so code can conditionally compile tray support.

# Custom VAPI directory for mosquitto bindings
add_project_arguments(
    ['--vapidir', meson.project_source_root() / 'vapi'],
    language: 'vala',
)

# Compile flags for optional deps
vala_args = ['--pkg', 'mosquitto']
c_args = []
if ayatana_dep.found()
    vala_args += '-D'
    vala_args += 'HAVE_AYATANA'
    c_args += '-DHAVE_AYATANA=1'
endif

# GResource
resources = gnome.compile_resources(
    'mu-resources',
    'data/mu.gresource.xml',
    source_dir: 'data',
    c_name: 'mu',
)

# Sources
sources = files(
    'src/main.vala',
    'src/application.vala',
    'src/window.vala',
    'src/ui/now_playing_view.vala',
    'src/mqtt/topics.vala',
    'src/mqtt/mqtt_client.vala',
)

# Build
executable(
    'media-utopia',
    sources,
    resources,
    vala_args: vala_args,
    c_args: c_args,
    dependencies: [
        gtk4_dep,
        adw_dep,
        gstreamer_dep,
        gstreamer_audio_dep,
        json_glib_dep,
        libsoup_dep,
        mosquitto_c_dep,
    ],
    install: true,
)

# Install GSettings schema
install_data(
    'data/com.mediautopia.desktop.gschema.xml',
    install_dir: get_option('datadir') / 'glib-2.0' / 'schemas',
)

# Install desktop entry
install_data(
    'data/com.mediautopia.desktop.desktop.in',
    rename: 'com.mediautopia.desktop.desktop',
    install_dir: get_option('datadir') / 'applications',
)
