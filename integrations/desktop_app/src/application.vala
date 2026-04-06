/* application.vala — Mu.Application : Adw.Application */

namespace Mu {

    public class Application : Adw.Application {

        /* ---- Service graph ---- */
        private GLib.Settings app_settings;
        private MqttClient mqtt;
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;
        private NodeRepository node_repo;
        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_renderer_repo;
        private LibraryRepository library_repo;
        private PlaylistRepository playlist_repo;
        private LocalRenderer local_renderer;

        /* ---- Identity ---- */
        private string controller_id;
        private string local_renderer_id;
        private string identity;
        private bool local_renderer_started = false;

        public Application () {
            Object (
                application_id: "com.mediautopia.desktop",
                flags: ApplicationFlags.DEFAULT_FLAGS
            );
        }

        protected override void startup () {
            base.startup ();
            load_css ();
            init_services ();
        }

        protected override void activate () {
            var window = this.active_window;
            if (window == null) {
                window = new Mu.Window (
                    this,
                    mqtt,
                    correlator,
                    lease_mgr,
                    node_repo,
                    state_repo,
                    active_renderer_repo,
                    library_repo,
                    playlist_repo
                );
            }
            window.present ();
        }

        protected override void shutdown () {
            /* Clean up services */
            local_renderer.stop ();
            lease_mgr.release_all ();
            correlator.cleanup ();
            node_repo.stop ();
            state_repo.stop ();
            mqtt.disconnect_from_broker ();

            base.shutdown ();
        }

        private void init_services () {
            var hostname = Environment.get_host_name ();
            controller_id = "mu:controller:desktop:%s".printf (hostname);
            local_renderer_id = "mu:renderer:gstreamer:desktop:%s:default".printf (hostname);

            /* 1. GSettings */
            app_settings = new GLib.Settings ("com.mediautopia.desktop");

            /* Identity: use GSettings "identity", fall back to hostname */
            var stored_identity = app_settings.get_string ("identity");
            identity = (stored_identity.length > 0) ? stored_identity : hostname;

            /* 2. MQTT client */
            mqtt = new MqttClient (controller_id);

            /* 3. Command correlator */
            correlator = new CommandCorrelator (mqtt);
            correlator.setup (MqttTopics.BASE, controller_id, identity);

            /* 4. Lease manager */
            lease_mgr = new LeaseManager (correlator);
            lease_mgr.start_renewal ();

            /* 5. Node repository */
            node_repo = new NodeRepository (mqtt);
            node_repo.start ();

            /* 6. Renderer state repository */
            state_repo = new RendererStateRepository (mqtt);
            state_repo.start ();

            /* 7. Active renderer repository */
            active_renderer_repo = new ActiveRendererRepository (app_settings, local_renderer_id);

            /* 8. Library repository */
            library_repo = new LibraryRepository (correlator, lease_mgr);

            /* 9. Playlist repository */
            playlist_repo = new PlaylistRepository (correlator, lease_mgr);

            /* 10. Local GStreamer renderer */
            local_renderer = new LocalRenderer (mqtt, local_renderer_id, identity);
            state_repo.register_local_source (local_renderer_id);
            local_renderer.state_updated.connect ((state) => {
                state_repo.update_local_state (state);
            });

            /* Temporary logging for MQTT discovery verification */
            node_repo.node_added.connect ((presence) => {
                message ("NODE DISCOVERED: %s (kind=%s, name=%s)",
                         presence.node_id, presence.kind, presence.name);
            });
            mqtt.connection_changed.connect ((conn_state) => {
                message ("MQTT connection state: %s", conn_state.to_string ());
                if (conn_state == ConnectionState.CONNECTED && !local_renderer_started) {
                    local_renderer_started = true;

                    /* Start local renderer and register presence on first connect */
                    local_renderer.start ();

                    /* Register local renderer presence with node repo (bypasses MQTT) */
                    var local_presence = new Presence ();
                    local_presence.node_id = local_renderer_id;
                    local_presence.kind = "renderer";
                    local_presence.name = identity;
                    local_presence.source = "desktop_app";
                    var caps = new Json.Object ();
                    caps.set_boolean_member ("seek", true);
                    caps.set_boolean_member ("volume", true);
                    local_presence.caps = caps;
                    local_presence.ts = GLib.get_real_time () / 1000;
                    node_repo.register_local (local_presence);
                }
            });

            /* Connect to MQTT broker */
            connect_mqtt_broker ();
        }

        private void connect_mqtt_broker () {
            var broker_url = app_settings.get_string ("broker-url");
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

            /* Set LWT: empty payload on local renderer presence topic (retained, QoS 1)
             * so other nodes see us go offline on unexpected disconnect */
            var lwt_topic = MqttTopics.presence (local_renderer_id);
            mqtt.set_will (lwt_topic, "", 1, true);

            message ("Connecting to MQTT broker at %s:%d (controller=%s)", host, port, controller_id);
            mqtt.connect_to_broker (host, port);
        }

        private void load_css () {
            var provider = new Gtk.CssProvider ();
            provider.load_from_resource ("/com/mediautopia/desktop/style.css");
            Gtk.StyleContext.add_provider_for_display (
                Gdk.Display.get_default (),
                provider,
                Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION
            );
        }
    }
}
