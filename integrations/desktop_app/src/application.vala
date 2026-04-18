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
        private ZoneStateRepository zone_state_repo;
        private ActiveRendererRepository active_renderer_repo;
        private LibraryRepository library_repo;
        private PlaylistRepository playlist_repo;
        private LocalRenderer local_renderer;

        /* ---- Platform services ---- */
        private Mpris2 mpris2;
        private TrayManager tray_mgr;
        private Notifications notifications;

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
            register_notification_actions ();
            register_keyboard_shortcuts ();
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
                    zone_state_repo,
                    active_renderer_repo,
                    library_repo,
                    playlist_repo,
                    local_renderer
                );
            }
            window.present ();
        }

        protected override void shutdown () {
            /* Clean up platform services */
            notifications.stop ();
            tray_mgr.stop ();
            mpris2.stop ();

            /* Clean up core services */
            local_renderer.stop ();
            lease_mgr.release_all ();
            correlator.cleanup ();
            node_repo.stop ();
            state_repo.stop ();
            zone_state_repo.stop ();
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

            /* 7. Zone state repository */
            zone_state_repo = new ZoneStateRepository (mqtt);
            zone_state_repo.start ();

            /* 8. Active renderer repository */
            active_renderer_repo = new ActiveRendererRepository (app_settings, local_renderer_id);

            /* 9. Library repository */
            library_repo = new LibraryRepository (correlator, lease_mgr);

            /* 10. Playlist repository */
            playlist_repo = new PlaylistRepository (correlator, lease_mgr);

            /* 11. Local GStreamer renderer */
            local_renderer = new LocalRenderer (mqtt, local_renderer_id, identity);
            state_repo.register_local_source (local_renderer_id);
            local_renderer.state_updated.connect ((state) => {
                state_repo.update_local_state (state);
            });

            node_repo.node_removed.connect ((node_id) => {
                if (node_id == active_renderer_repo.active_renderer_id) {
                    active_renderer_repo.set_active_renderer (local_renderer_id);
                }
            });

            /* Temporary logging for MQTT discovery verification */
            node_repo.node_added.connect ((presence) => {
                message ("NODE DISCOVERED: %s (kind=%s, name=%s)",
                         presence.node_id, presence.kind, presence.name);
            });
            mqtt.connection_changed.connect ((conn_state) => {
                message ("MQTT connection state: %s", conn_state.to_string ());
                if (conn_state == ConnectionState.CONNECTED) {
                    if (!local_renderer_started) {
                        local_renderer_started = true;
                        local_renderer.start ();
                    }

                    local_renderer.on_broker_connected ();
                    register_local_renderer_presence ();
                }
            });

            /* 12. MPRIS2 D-Bus media player interface */
            mpris2 = new Mpris2 (this, state_repo, active_renderer_repo,
                correlator, lease_mgr);
            mpris2.start ();

            /* 13. Tray manager (keep-alive when window hidden) */
            tray_mgr = new TrayManager (this, app_settings);

            /* 14. Desktop notifications on track change */
            notifications = new Notifications (this, state_repo, active_renderer_repo);

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

        private void register_local_renderer_presence () {
            var local_presence = new Presence ();
            local_presence.node_id = local_renderer_id;
            local_presence.kind = "renderer";
            local_presence.name = identity;
            var caps = new Json.Object ();
            caps.set_boolean_member ("seek", true);
            caps.set_boolean_member ("volume", true);
            caps.set_boolean_member ("queue", true);
            caps.set_boolean_member ("shuffle", true);
            caps.set_boolean_member ("repeat", true);
            local_presence.caps = caps;
            local_presence.ts = GLib.get_real_time () / 1000;
            node_repo.register_local (local_presence);
        }

        /**
         * Register GLib.SimpleAction handlers for notification buttons
         * and MPRIS2 Raise action.
         */
        private void register_notification_actions () {
            /* app.next — skip to next track */
            var next_action = new SimpleAction ("next", null);
            next_action.activate.connect (() => {
                send_active_command ("playback.next", new Json.Object ());
            });
            add_action (next_action);

            /* app.play-pause — toggle playback */
            var play_pause_action = new SimpleAction ("play-pause", null);
            play_pause_action.activate.connect (() => {
                var renderer_id = active_renderer_repo.active_renderer_id;
                if (renderer_id.length == 0) return;

                var state = state_repo.get_state (renderer_id);
                if (state != null && state.playback != null &&
                    state.playback.status == "playing") {
                    send_active_command ("playback.pause", new Json.Object ());
                } else {
                    send_active_command ("playback.play", PlaybackBodies.play ());
                }
            });
            add_action (play_pause_action);

            /* app.raise-window — bring the window to front */
            var raise_action = new SimpleAction ("raise-window", null);
            raise_action.activate.connect (() => {
                activate ();
            });
            add_action (raise_action);
        }

        /**
         * Register keyboard shortcut actions and bind accelerators.
         */
        private void register_keyboard_shortcuts () {
            /* app.previous — previous track */
            var prev_action = new SimpleAction ("previous", null);
            prev_action.activate.connect (() => {
                send_active_command ("playback.prev", new Json.Object ());
            });
            add_action (prev_action);

            /* app.seek-forward — seek +10s */
            var seek_fwd_action = new SimpleAction ("seek-forward", null);
            seek_fwd_action.activate.connect (() => {
                var renderer_id = active_renderer_repo.active_renderer_id;
                if (renderer_id.length == 0) return;

                var state = state_repo.get_state (renderer_id);
                if (state != null && state.playback != null) {
                    var target = state.playback.position_ms + 10000;
                    if (state.playback.duration_ms > 0) {
                        target = int64.min (target, state.playback.duration_ms);
                    }
                    send_active_command ("playback.seek",
                        PlaybackBodies.seek (target));
                }
            });
            add_action (seek_fwd_action);

            /* app.seek-back — seek -10s */
            var seek_back_action = new SimpleAction ("seek-back", null);
            seek_back_action.activate.connect (() => {
                var renderer_id = active_renderer_repo.active_renderer_id;
                if (renderer_id.length == 0) return;

                var state = state_repo.get_state (renderer_id);
                if (state != null && state.playback != null) {
                    var target = int64.max (0, state.playback.position_ms - 10000);
                    send_active_command ("playback.seek",
                        PlaybackBodies.seek (target));
                }
            });
            add_action (seek_back_action);

            /* app.volume-up — increase volume by 5% */
            var vol_up_action = new SimpleAction ("volume-up", null);
            vol_up_action.activate.connect (() => {
                var renderer_id = active_renderer_repo.active_renderer_id;
                if (renderer_id.length == 0) return;

                var state = state_repo.get_state (renderer_id);
                if (state != null && state.playback != null) {
                    var target = double.min (1.0, state.playback.volume + 0.05);
                    send_active_command ("playback.setVolume",
                        PlaybackBodies.set_volume (target));
                }
            });
            add_action (vol_up_action);

            /* app.volume-down — decrease volume by 5% */
            var vol_down_action = new SimpleAction ("volume-down", null);
            vol_down_action.activate.connect (() => {
                var renderer_id = active_renderer_repo.active_renderer_id;
                if (renderer_id.length == 0) return;

                var state = state_repo.get_state (renderer_id);
                if (state != null && state.playback != null) {
                    var target = double.max (0.0, state.playback.volume - 0.05);
                    send_active_command ("playback.setVolume",
                        PlaybackBodies.set_volume (target));
                }
            });
            add_action (vol_down_action);

            /* app.mute-toggle — toggle mute */
            var mute_action = new SimpleAction ("mute-toggle", null);
            mute_action.activate.connect (() => {
                var renderer_id = active_renderer_repo.active_renderer_id;
                if (renderer_id.length == 0) return;

                var state = state_repo.get_state (renderer_id);
                if (state != null && state.playback != null) {
                    send_active_command ("playback.setMute",
                        PlaybackBodies.set_mute (!state.playback.mute));
                }
            });
            add_action (mute_action);

            /* app.quit-or-hide — quit or hide to tray */
            var quit_action = new SimpleAction ("quit-or-hide", null);
            quit_action.activate.connect (() => {
                var window = active_window;
                if (window != null && app_settings.get_boolean ("close-to-tray")) {
                    window.set_visible (false);
                } else {
                    quit ();
                }
            });
            add_action (quit_action);

            /* Bind keyboard accelerators to actions */
            set_accels_for_action ("app.play-pause", { "space" });
            set_accels_for_action ("app.next", { "n" });
            set_accels_for_action ("app.previous", { "p" });
            set_accels_for_action ("app.seek-forward", { "Right" });
            set_accels_for_action ("app.seek-back", { "Left" });
            set_accels_for_action ("app.volume-up", { "Up" });
            set_accels_for_action ("app.volume-down", { "Down" });
            set_accels_for_action ("app.mute-toggle", { "m" });
            set_accels_for_action ("app.quit-or-hide", { "<Control>q" });
        }

        /**
         * Send a fire-and-forget command to the active renderer.
         * Shared helper for notification action handlers.
         */
        private void send_active_command (string cmd_type, Json.Object body) {
            var renderer_id = active_renderer_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            lease_mgr.ensure_lease.begin (renderer_id, (obj, res) => {
                var lease = lease_mgr.ensure_lease.end (res);
                if (lease != null) {
                    correlator.send_fire_and_forget (renderer_id, cmd_type, body, lease);
                }
            });
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
