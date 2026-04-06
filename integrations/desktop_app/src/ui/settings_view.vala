/* settings_view.vala — Mu.SettingsView : Adw.PreferencesPage */

namespace Mu {

    public class SettingsView : Adw.PreferencesPage {

        private GLib.Settings settings;
        private MqttClient mqtt;

        /* Widgets we need to update dynamically */
        private Gtk.Label status_label;
        private Adw.EntryRow broker_row;

        public SettingsView (GLib.Settings settings, MqttClient mqtt) {
            this.settings = settings;
            this.mqtt = mqtt;

            build_ui ();

            /* Wire connection status updates */
            update_status (mqtt.connection_state);
            mqtt.connection_changed.connect (update_status);
        }

        private void build_ui () {
            title = "Settings";
            icon_name = "emblem-system-symbolic";

            /* --- Group 1: Connection --- */
            var conn_group = new Adw.PreferencesGroup ();
            conn_group.title = "Connection";
            conn_group.description = "MQTT broker settings for Media Utopia communication";

            /* Broker URL */
            broker_row = new Adw.EntryRow ();
            broker_row.title = "Broker URL";
            settings.bind ("broker-url", broker_row, "text",
                           GLib.SettingsBindFlags.DEFAULT);
            broker_row.entry_activated.connect (trigger_reconnect);
            conn_group.add (broker_row);

            /* Identity */
            var identity_row = new Adw.EntryRow ();
            identity_row.title = "Identity";
            settings.bind ("identity", identity_row, "text",
                           GLib.SettingsBindFlags.DEFAULT);
            conn_group.add (identity_row);

            /* Connection status row */
            var status_row = new Adw.ActionRow ();
            status_row.title = "Status";

            status_label = new Gtk.Label ("Disconnected");
            status_label.add_css_class ("connection-disconnected");
            status_label.valign = Gtk.Align.CENTER;
            status_row.add_suffix (status_label);

            conn_group.add (status_row);

            /* Reconnect button */
            var reconnect_row = new Adw.ActionRow ();
            reconnect_row.title = "Reconnect";
            reconnect_row.subtitle = "Disconnect and reconnect to the broker";

            var reconnect_btn = new Gtk.Button.with_label ("Reconnect");
            reconnect_btn.add_css_class ("suggested-action");
            reconnect_btn.valign = Gtk.Align.CENTER;
            reconnect_btn.clicked.connect (trigger_reconnect);
            reconnect_row.add_suffix (reconnect_btn);
            reconnect_row.activatable_widget = reconnect_btn;

            conn_group.add (reconnect_row);

            add (conn_group);

            /* --- Group 2: Appearance --- */
            var appearance_group = new Adw.PreferencesGroup ();
            appearance_group.title = "Appearance";

            var vis_row = new Adw.SwitchRow ();
            vis_row.title = "Visualizer";
            vis_row.subtitle = "Show audio visualizer on the Now Playing view";
            settings.bind ("visualizer-enabled", vis_row, "active",
                           GLib.SettingsBindFlags.DEFAULT);
            appearance_group.add (vis_row);

            add (appearance_group);

            /* --- Group 3: Behavior --- */
            var behavior_group = new Adw.PreferencesGroup ();
            behavior_group.title = "Behavior";

            var tray_row = new Adw.SwitchRow ();
            tray_row.title = "Close to Tray";
            tray_row.subtitle = "Closing the window hides to tray instead of quitting";
            settings.bind ("close-to-tray", tray_row, "active",
                           GLib.SettingsBindFlags.DEFAULT);
            behavior_group.add (tray_row);

            add (behavior_group);

            /* --- Group 4: About --- */
            var about_group = new Adw.PreferencesGroup ();
            about_group.title = "About";

            var version_row = new Adw.ActionRow ();
            version_row.title = "Version";
            var version_label = new Gtk.Label ("0.1.0");
            version_label.add_css_class ("text-secondary");
            version_label.valign = Gtk.Align.CENTER;
            version_row.add_suffix (version_label);
            about_group.add (version_row);

            var desc_row = new Adw.ActionRow ();
            desc_row.title = "Media Utopia";
            desc_row.subtitle = "A distributed media system for local-first music playback and control";
            about_group.add (desc_row);

            add (about_group);
        }

        private void trigger_reconnect () {
            var broker_url = settings.get_string ("broker-url");
            string host;
            int port = 1883;

            /* Parse "mqtt://host:port" or plain "host" or "host:port" */
            var url = broker_url;
            if (url.has_prefix ("mqtt://")) {
                url = url.substring (7);
            }

            var colon_pos = url.last_index_of_char (':');
            if (colon_pos > 0) {
                host = url.substring (0, colon_pos);
                var port_str = url.substring (colon_pos + 1);
                port = int.parse (port_str);
                if (port <= 0 || port > 65535) {
                    port = 1883;
                }
            } else {
                host = url;
            }

            message ("SettingsView: reconnecting to %s:%d", host, port);
            mqtt.disconnect_from_broker ();
            mqtt.connect_to_broker (host, port);
        }

        private void update_status (ConnectionState state) {
            if (status_label == null) return;

            string text;
            string css_class;

            switch (state) {
                case ConnectionState.CONNECTED:
                    text = "Connected";
                    css_class = "connection-connected";
                    break;
                case ConnectionState.CONNECTING:
                    text = "Connecting\u2026";
                    css_class = "connection-connecting";
                    break;
                case ConnectionState.RECONNECTING:
                    text = "Reconnecting\u2026";
                    css_class = "connection-reconnecting";
                    break;
                default:
                    text = "Disconnected";
                    css_class = "connection-disconnected";
                    break;
            }

            status_label.label = text;

            /* Remove all connection CSS classes and apply the current one */
            status_label.remove_css_class ("connection-connected");
            status_label.remove_css_class ("connection-connecting");
            status_label.remove_css_class ("connection-reconnecting");
            status_label.remove_css_class ("connection-disconnected");
            status_label.add_css_class (css_class);
        }
    }
}
