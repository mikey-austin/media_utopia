/*
 * Plugin configuration backed by GSettings.
 */

namespace Mu {

    public class Config : Object {

        private GLib.Settings? settings = null;

        public string broker_host { get; private set; default = "localhost"; }
        public int    broker_port { get; private set; default = 1883; }
        public string broker_username { get; private set; default = ""; }
        public string broker_password { get; private set; default = ""; }
        public string topic_base { get; private set; default = "mu/v1"; }
        public string node_name { get; private set; default = "Rhythmbox"; }
        public bool   renderer_enabled { get; private set; default = true; }
        public bool   controller_enabled { get; private set; default = true; }
        public bool   library_enabled { get; private set; default = true; }

        public Config () {
            var schema_source = GLib.SettingsSchemaSource.get_default ();
            if (schema_source != null) {
                var schema = schema_source.lookup ("org.gnome.rhythmbox.plugins.mu", true);
                if (schema != null) {
                    settings = new GLib.Settings ("org.gnome.rhythmbox.plugins.mu");
                    load_from_settings ();
                    settings.changed.connect (on_settings_changed);
                }
            }

            if (settings == null) {
                message ("MU plugin: GSettings schema not installed, using defaults");
            }
        }

        private void load_from_settings () {
            if (settings == null) return;
            broker_host = settings.get_string ("broker-host");
            broker_port = settings.get_int ("broker-port");
            broker_username = settings.get_string ("broker-username");
            broker_password = settings.get_string ("broker-password");
            topic_base = settings.get_string ("topic-base");
            node_name = settings.get_string ("node-name");
            renderer_enabled = settings.get_boolean ("renderer-enabled");
            controller_enabled = settings.get_boolean ("controller-enabled");
            library_enabled = settings.get_boolean ("library-enabled");
        }

        private void on_settings_changed (string key) {
            load_from_settings ();
        }

        /**
         * Build the renderer node ID based on hostname.
         */
        public string build_node_id () {
            string hostname = GLib.Environment.get_host_name ();
            string user = GLib.Environment.get_user_name ();
            return @"mu:renderer:rhythmbox:$(user)@$(hostname):default";
        }

        /**
         * Build the controller identity string.
         */
        public string build_controller_id () {
            string hostname = GLib.Environment.get_host_name ();
            string user = GLib.Environment.get_user_name ();
            return @"rhythmbox@$(user)@$(hostname)";
        }
    }
}
