/* window.vala — Mu.Window : Adw.ApplicationWindow with sidebar nav */

namespace Mu {

    public class Window : Adw.ApplicationWindow {

        private Gtk.ListBox nav_list;
        private Gtk.Stack content_stack;
        private GLib.Settings settings;

        /* Navigation items: name, icon, stack-child-name */
        private struct NavItem {
            string label;
            string icon_name;
            string child_name;
        }

        private const NavItem[] NAV_ITEMS = {
            { "Now Playing", "media-playback-start-symbolic", "now-playing" },
            { "Queue", "view-list-symbolic", "queue" },
            { "Library", "library-music-symbolic", "library" },
            { "Renderers", "audio-speakers-symbolic", "renderers" },
            { "Zones", "network-workgroup-symbolic", "zones" }
        };

        public Window (Mu.Application app) {
            Object (
                application: app,
                title: "Media Utopia"
            );
        }

        construct {
            settings = new GLib.Settings ("com.mediautopia.desktop");

            /* Restore saved geometry */
            default_width = settings.get_int ("window-width");
            default_height = settings.get_int ("window-height");
            if (settings.get_boolean ("window-maximized")) {
                maximize ();
            }

            /* Track window state changes */
            notify["default-width"].connect (save_window_size);
            notify["default-height"].connect (save_window_size);
            notify["maximized"].connect (() => {
                settings.set_boolean ("window-maximized", maximized);
            });

            /* Close-to-tray: override close request */
            close_request.connect (() => {
                if (settings.get_boolean ("close-to-tray")) {
                    set_visible (false);
                    return true;  /* prevent destruction */
                }
                return false;  /* allow normal close */
            });

            build_ui ();
        }

        private void save_window_size () {
            if (!maximized) {
                settings.set_int ("window-width", default_width);
                settings.set_int ("window-height", default_height);
            }
        }

        private void build_ui () {
            /* Main horizontal layout: sidebar | content */
            var paned = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 0);

            /* --- Sidebar --- */
            var sidebar = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            sidebar.add_css_class ("sidebar");
            sidebar.width_request = 210;

            /* MU Logo */
            var logo_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            logo_box.add_css_class ("sidebar-logo");
            logo_box.halign = Gtk.Align.START;

            var logo = new Gtk.Image.from_resource ("/com/mediautopia/desktop/icons/mu-motif.svg");
            logo.pixel_size = 32;
            logo_box.append (logo);

            var app_label = new Gtk.Label ("Media Utopia");
            app_label.add_css_class ("heading-small");
            logo_box.append (app_label);

            sidebar.append (logo_box);

            /* Separator below logo */
            var sep = new Gtk.Separator (Gtk.Orientation.HORIZONTAL);
            sep.margin_start = 12;
            sep.margin_end = 12;
            sep.margin_top = 4;
            sep.margin_bottom = 4;
            sidebar.append (sep);

            /* Navigation list */
            nav_list = new Gtk.ListBox ();
            nav_list.selection_mode = Gtk.SelectionMode.SINGLE;
            nav_list.add_css_class ("navigation-sidebar");
            nav_list.vexpand = true;

            for (int i = 0; i < NAV_ITEMS.length; i++) {
                var row = make_nav_row (NAV_ITEMS[i].label, NAV_ITEMS[i].icon_name);
                nav_list.append (row);
            }

            sidebar.append (nav_list);

            /* Settings pinned at bottom */
            var settings_sep = new Gtk.Separator (Gtk.Orientation.HORIZONTAL);
            settings_sep.margin_start = 12;
            settings_sep.margin_end = 12;
            sidebar.append (settings_sep);

            var settings_list = new Gtk.ListBox ();
            settings_list.selection_mode = Gtk.SelectionMode.NONE;
            settings_list.add_css_class ("navigation-sidebar");
            var settings_row = make_nav_row ("Settings", "emblem-system-symbolic");
            settings_list.append (settings_row);
            settings_list.row_activated.connect (() => {
                /* Clear main nav selection when settings clicked */
                nav_list.unselect_all ();
                content_stack.visible_child_name = "settings";
            });
            sidebar.append (settings_list);

            paned.append (sidebar);

            /* Vertical separator between sidebar and content */
            paned.append (new Gtk.Separator (Gtk.Orientation.VERTICAL));

            /* --- Content area --- */
            content_stack = new Gtk.Stack ();
            content_stack.add_css_class ("content-area");
            content_stack.hexpand = true;
            content_stack.vexpand = true;
            content_stack.transition_type = Gtk.StackTransitionType.CROSSFADE;
            content_stack.transition_duration = 200;

            /* Add view stubs */
            content_stack.add_named (new Mu.NowPlayingView (), "now-playing");
            content_stack.add_named (make_placeholder ("Queue"), "queue");
            content_stack.add_named (make_placeholder ("Library"), "library");
            content_stack.add_named (make_placeholder ("Renderers"), "renderers");
            content_stack.add_named (make_placeholder ("Zones"), "zones");
            content_stack.add_named (make_placeholder ("Settings"), "settings");

            content_stack.visible_child_name = "now-playing";

            paned.append (content_stack);

            /* Wire sidebar selection to stack */
            nav_list.row_selected.connect ((row) => {
                if (row == null) return;
                var index = row.get_index ();
                if (index >= 0 && index < NAV_ITEMS.length) {
                    content_stack.visible_child_name = NAV_ITEMS[index].child_name;
                }
            });

            /* Select first row by default */
            nav_list.select_row (nav_list.get_row_at_index (0));

            this.content = paned;
        }

        private Gtk.Box make_nav_row (string label, string icon_name) {
            var box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            box.margin_start = 4;
            box.margin_end = 4;

            var icon = new Gtk.Image.from_icon_name (icon_name);
            icon.add_css_class ("nav-icon");
            box.append (icon);

            var lbl = new Gtk.Label (label);
            lbl.add_css_class ("nav-label");
            lbl.halign = Gtk.Align.START;
            lbl.hexpand = true;
            box.append (lbl);

            return box;
        }

        private Gtk.Widget make_placeholder (string view_name) {
            var label = new Gtk.Label (view_name);
            label.add_css_class ("heading-large");
            label.halign = Gtk.Align.CENTER;
            label.valign = Gtk.Align.CENTER;
            label.hexpand = true;
            label.vexpand = true;
            return label;
        }
    }
}
