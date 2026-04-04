/*
 * Media Utopia Rhythmbox plugin — entry point.
 *
 * Implements Peas.Activatable to integrate with Rhythmbox's plugin system.
 * On activation:
 *   1. Loads configuration (MQTT broker, role toggles, etc.)
 *   2. Connects to MQTT broker
 *   3. Starts node registry (presence discovery)
 *   4. Starts enabled roles: renderer, controller, library browser
 *
 * On deactivation, everything is torn down cleanly.
 */

namespace Mu {

    public class Plugin : Peas.ExtensionBase, Peas.Activatable, PeasGtk.Configurable {

        public Object object { owned get; construct; }

        private Config? config = null;
        private MqttClient? mqtt = null;
        private NodeRegistry? registry = null;
        private Renderer? renderer = null;
        private Controller? controller = null;
        private HashTable<string, LibrarySource>? library_sources = null;
        private Mu.EntryType? shared_entry_type = null;

        public void activate () {
            var shell = (RB.Shell) object;

            /* Load configuration. */
            config = new Config ();

            /* Build client ID. */
            string client_id = config.build_controller_id ();

            /* Create MQTT client. */
            mqtt = new MqttClient (
                client_id,
                config.broker_host,
                config.broker_port,
                config.broker_username.length > 0 ? config.broker_username : null,
                config.broker_password.length > 0 ? config.broker_password : null
            );

            /* Register in the global client registry for callback routing. */
            /* (The MqttClient.start() method will call Mosquitto.Client which
             * needs to be registered before connect callbacks fire.) */
            mqtt.connected.connect (on_mqtt_connected);
            mqtt.disconnected.connect (on_mqtt_disconnected);

            /* Set LWT to clear presence on ungraceful disconnect. */
            if (config.renderer_enabled) {
                string presence_topic = topic_presence (config.topic_base, config.build_node_id ());
                mqtt.set_will (presence_topic, {}, 1, true);
            }

            /* Start the MQTT event loop. */
            mqtt.start ();

            /* Subscribe to the reply topic so command replies are received. */
            string reply_topic = topic_reply (config.topic_base, config.build_controller_id ());
            mqtt.subscribe (reply_topic, 1);

            /* Shared entry type for all MU tracks. */
            shared_entry_type = new Mu.EntryType ();
            shell.db.register_entry_type (shared_entry_type);

            /* Node registry — discovers all MU nodes on the network. */
            registry = new NodeRegistry (mqtt, config.topic_base);
            registry.start ();

            /* Start enabled roles. */
            if (config.renderer_enabled) {
                renderer = new Renderer (shell, mqtt, config, shared_entry_type);
                renderer.start ();
            }

            if (config.controller_enabled) {
                controller = new Controller (shell, mqtt, registry, config);
                controller.start ();
            }

            if (config.library_enabled) {
                library_sources = new HashTable<string, LibrarySource> (str_hash, str_equal);
                registry.node_added.connect (on_node_added);
                registry.node_removed.connect (on_node_removed);
            }

            message ("Media Utopia plugin activated (broker: %s:%d)", config.broker_host, config.broker_port);
        }

        public void deactivate () {
            /* Stop roles in reverse order. */
            if (library_sources != null) {
                registry.node_added.disconnect (on_node_added);
                registry.node_removed.disconnect (on_node_removed);
                /* Remove library sources from shell. */
                var shell = (RB.Shell) object;
                library_sources.foreach ((_, source) => {
                    shell.remove_widget (source, RB.ShellUILocation.SIDEBAR);
                });
                library_sources = null;
            }

            if (controller != null) {
                controller.stop ();
                controller = null;
            }

            if (renderer != null) {
                renderer.stop ();
                renderer = null;
            }

            if (registry != null) {
                registry.stop ();
                registry = null;
            }

            if (mqtt != null) {
                mqtt.stop ();
                mqtt = null;
            }

            config = null;
            message ("Media Utopia plugin deactivated");
        }

        public void update_state () {
            /* Required by Peas.Activatable but not needed. */
        }

        /* ── Configuration dialog ───────────────────────────── */

        public Gtk.Widget create_configure_widget () {
            var grid = new Gtk.Grid ();
            grid.column_spacing = 12;
            grid.row_spacing = 8;
            grid.margin = 12;

            var settings = get_settings ();
            int row = 0;

            /* Section: MQTT Broker */
            var broker_header = new Gtk.Label (null);
            broker_header.set_markup ("<b>MQTT Broker</b>");
            broker_header.halign = Gtk.Align.START;
            grid.attach (broker_header, 0, row++, 2, 1);

            /* Host */
            var host_label = new Gtk.Label ("Host:");
            host_label.halign = Gtk.Align.END;
            var host_entry = new Gtk.Entry ();
            host_entry.hexpand = true;
            host_entry.text = settings.get_string ("broker-host");
            host_entry.changed.connect (() => {
                settings.set_string ("broker-host", host_entry.text);
            });
            grid.attach (host_label, 0, row, 1, 1);
            grid.attach (host_entry, 1, row++, 1, 1);

            /* Port */
            var port_label = new Gtk.Label ("Port:");
            port_label.halign = Gtk.Align.END;
            var port_spin = new Gtk.SpinButton.with_range (1, 65535, 1);
            port_spin.value = settings.get_int ("broker-port");
            port_spin.value_changed.connect (() => {
                settings.set_int ("broker-port", (int) port_spin.value);
            });
            grid.attach (port_label, 0, row, 1, 1);
            grid.attach (port_spin, 1, row++, 1, 1);

            /* Username */
            var user_label = new Gtk.Label ("Username:");
            user_label.halign = Gtk.Align.END;
            var user_entry = new Gtk.Entry ();
            user_entry.placeholder_text = "anonymous";
            user_entry.text = settings.get_string ("broker-username");
            user_entry.changed.connect (() => {
                settings.set_string ("broker-username", user_entry.text);
            });
            grid.attach (user_label, 0, row, 1, 1);
            grid.attach (user_entry, 1, row++, 1, 1);

            /* Password */
            var pass_label = new Gtk.Label ("Password:");
            pass_label.halign = Gtk.Align.END;
            var pass_entry = new Gtk.Entry ();
            pass_entry.visibility = false;
            pass_entry.input_purpose = Gtk.InputPurpose.PASSWORD;
            pass_entry.text = settings.get_string ("broker-password");
            pass_entry.changed.connect (() => {
                settings.set_string ("broker-password", pass_entry.text);
            });
            grid.attach (pass_label, 0, row, 1, 1);
            grid.attach (pass_entry, 1, row++, 1, 1);

            /* Section: Protocol */
            var proto_header = new Gtk.Label (null);
            proto_header.set_markup ("<b>Protocol</b>");
            proto_header.halign = Gtk.Align.START;
            proto_header.margin_top = 8;
            grid.attach (proto_header, 0, row++, 2, 1);

            /* Topic base */
            var topic_label = new Gtk.Label ("Topic base:");
            topic_label.halign = Gtk.Align.END;
            var topic_entry = new Gtk.Entry ();
            topic_entry.text = settings.get_string ("topic-base");
            topic_entry.changed.connect (() => {
                settings.set_string ("topic-base", topic_entry.text);
            });
            grid.attach (topic_label, 0, row, 1, 1);
            grid.attach (topic_entry, 1, row++, 1, 1);

            /* Section: Renderer */
            var renderer_header = new Gtk.Label (null);
            renderer_header.set_markup ("<b>Renderer</b>");
            renderer_header.halign = Gtk.Align.START;
            renderer_header.margin_top = 8;
            grid.attach (renderer_header, 0, row++, 2, 1);

            /* Node name */
            var name_label = new Gtk.Label ("Display name:");
            name_label.halign = Gtk.Align.END;
            var name_entry = new Gtk.Entry ();
            name_entry.text = settings.get_string ("node-name");
            name_entry.changed.connect (() => {
                settings.set_string ("node-name", name_entry.text);
            });
            grid.attach (name_label, 0, row, 1, 1);
            grid.attach (name_entry, 1, row++, 1, 1);

            /* Section: Roles */
            var roles_header = new Gtk.Label (null);
            roles_header.set_markup ("<b>Enabled Roles</b>");
            roles_header.halign = Gtk.Align.START;
            roles_header.margin_top = 8;
            grid.attach (roles_header, 0, row++, 2, 1);

            /* Renderer toggle */
            var renderer_check = new Gtk.CheckButton.with_label (
                "Renderer — expose Rhythmbox as an MU playback target");
            renderer_check.active = settings.get_boolean ("renderer-enabled");
            renderer_check.toggled.connect (() => {
                settings.set_boolean ("renderer-enabled", renderer_check.active);
            });
            grid.attach (renderer_check, 0, row++, 2, 1);

            /* Controller toggle */
            var controller_check = new Gtk.CheckButton.with_label (
                "Controller — control remote MU renderers from Rhythmbox");
            controller_check.active = settings.get_boolean ("controller-enabled");
            controller_check.toggled.connect (() => {
                settings.set_boolean ("controller-enabled", controller_check.active);
            });
            grid.attach (controller_check, 0, row++, 2, 1);

            /* Library toggle */
            var library_check = new Gtk.CheckButton.with_label (
                "Library browser — browse MU libraries in the sidebar");
            library_check.active = settings.get_boolean ("library-enabled");
            library_check.toggled.connect (() => {
                settings.set_boolean ("library-enabled", library_check.active);
            });
            grid.attach (library_check, 0, row++, 2, 1);

            /* Restart note */
            var note = new Gtk.Label (null);
            note.set_markup (
                "<small><i>Changes take effect after disabling and re-enabling the plugin.</i></small>");
            note.halign = Gtk.Align.START;
            note.margin_top = 12;
            grid.attach (note, 0, row++, 2, 1);

            grid.show_all ();
            return grid;
        }

        private GLib.Settings? get_settings () {
            var schema_source = GLib.SettingsSchemaSource.get_default ();
            if (schema_source != null) {
                var schema = schema_source.lookup (
                    "org.gnome.rhythmbox.plugins.mu", true);
                if (schema != null) {
                    return new GLib.Settings ("org.gnome.rhythmbox.plugins.mu");
                }
            }
            return null;
        }

        /* ── MQTT lifecycle ─────────────────────────────────── */

        private void on_mqtt_connected () {
            message ("MU: MQTT connected to %s:%d", config.broker_host, config.broker_port);
        }

        private void on_mqtt_disconnected () {
            message ("MU: MQTT disconnected");
        }

        /* ── Library source management ──────────────────────── */

        private void on_node_added (string node_id) {
            if (library_sources == null) return;
            if (library_sources.contains (node_id)) return;

            var presence = registry.get_node (node_id);
            if (presence == null || presence.kind != "library") return;

            var shell = (RB.Shell) object;
            var source = new LibrarySource (shell, mqtt, config, presence,
                shared_entry_type, controller);

            /* Add to the sidebar under the "Shared" group. */
            var group = RB.DisplayPageGroup.get_by_id ("shared");
            shell.append_display_page (source, group);

            library_sources.insert (node_id, source);
            message ("MU: Added library source: %s", presence.name);
        }

        private void on_node_removed (string node_id) {
            if (library_sources == null) return;
            var source = library_sources.lookup (node_id);
            if (source == null) return;

            var shell = (RB.Shell) object;
            shell.remove_widget (source, RB.ShellUILocation.SIDEBAR);
            library_sources.remove (node_id);
            message ("MU: Removed library source: %s", node_id);
        }
    }
}

/* Module registration for libpeas. */
[ModuleInit]
public void peas_register_types (TypeModule module) {
    var obj_module = (Peas.ObjectModule) module;
    obj_module.register_extension_type (typeof (Peas.Activatable), typeof (Mu.Plugin));
    obj_module.register_extension_type (typeof (PeasGtk.Configurable), typeof (Mu.Plugin));
}
