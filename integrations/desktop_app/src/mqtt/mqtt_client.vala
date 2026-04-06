/* mqtt_client.vala — GObject wrapper around libmosquitto with GLib main loop integration. */

namespace Mu {

    public enum ConnectionState {
        DISCONNECTED,
        CONNECTING,
        CONNECTED,
        RECONNECTING;

        public string to_string () {
            switch (this) {
                case DISCONNECTED:  return "DISCONNECTED";
                case CONNECTING:    return "CONNECTING";
                case CONNECTED:     return "CONNECTED";
                case RECONNECTING:  return "RECONNECTING";
                default:            return "UNKNOWN";
            }
        }
    }

    public class MqttClient : GLib.Object {

        /* ---- Static instance for C callback routing ---- */
        private static MqttClient? _instance = null;

        /* ---- Signals ---- */
        public signal void connection_changed (ConnectionState state);
        public signal void message_received (string topic, uint8[] payload);

        /* ---- Properties ---- */
        public ConnectionState connection_state { get; private set; default = ConnectionState.DISCONNECTED; }
        public string client_id { get; construct; }

        /* ---- Internals ---- */
        private Mosquitto.Client? mosq = null;
        private uint loop_source_id = 0;
        private HashTable<string, int> subscriptions;
        private string? broker_host = null;
        private int broker_port = 1883;

        /* Reconnect backoff */
        private uint reconnect_source_id = 0;
        private uint reconnect_delay_ms = 2000;
        private const uint RECONNECT_DELAY_INITIAL_MS = 2000;
        private const uint RECONNECT_DELAY_MAX_MS = 30000;

        /* LWT state (set before connect) */
        private string? will_topic = null;
        private uint8[]? will_payload = null;
        private int will_qos = 0;
        private bool will_retain = false;

        private static bool lib_initialized = false;

        public MqttClient (string client_id) {
            Object (client_id: client_id);
        }

        construct {
            _instance = this;
            subscriptions = new HashTable<string, int> (str_hash, str_equal);

            if (!lib_initialized) {
                Mosquitto.lib_init ();
                lib_initialized = true;
            }

            mosq = new Mosquitto.Client (client_id, true, null);
            mosq.connect_callback_set (on_connect_cb);
            mosq.disconnect_callback_set (on_disconnect_cb);
            mosq.message_callback_set (on_message_cb);
        }

        ~MqttClient () {
            disconnect_from_broker ();
            mosq = null;
            if (_instance == this) {
                _instance = null;
            }
            /* Note: we intentionally do NOT call lib_cleanup here —
             * it is process-global and calling it while other instances
             * could theoretically exist is unsafe. */
        }

        /* ---- LWT ---- */

        public void set_will (string topic, string payload, int qos = 0, bool retain = false) {
            will_topic = topic;
            will_payload = payload.data;
            will_qos = qos;
            will_retain = retain;
        }

        /* ---- Connection ---- */

        public void connect_to_broker (string host, int port = 1883) {
            if (mosq == null) {
                warning ("MqttClient: mosquitto client not initialized");
                return;
            }

            broker_host = host;
            broker_port = port;
            reconnect_delay_ms = RECONNECT_DELAY_INITIAL_MS;

            /* Apply LWT if configured */
            if (will_topic != null && will_payload != null) {
                mosq.will_set (will_topic, will_payload, will_qos, will_retain);
            }

            set_state (ConnectionState.CONNECTING);

            var rc = mosq.connect (host, port, 60);
            if (rc != Mosquitto.ERR_SUCCESS) {
                warning ("MqttClient: connect failed: %s", Mosquitto.strerror (rc));
                schedule_reconnect ();
                return;
            }

            start_loop ();
        }

        public void disconnect_from_broker () {
            cancel_reconnect ();
            stop_loop ();

            if (mosq != null) {
                mosq.disconnect ();
            }

            set_state (ConnectionState.DISCONNECTED);
        }

        /* ---- Pub/Sub ---- */

        public void subscribe (string topic, int qos = 0) {
            subscriptions.set (topic, qos);

            if (connection_state == ConnectionState.CONNECTED && mosq != null) {
                int mid;
                var rc = mosq.subscribe (out mid, topic, qos);
                if (rc != Mosquitto.ERR_SUCCESS) {
                    warning ("MqttClient: subscribe '%s' failed: %s", topic, Mosquitto.strerror (rc));
                }
            }
        }

        public void unsubscribe (string topic) {
            subscriptions.remove (topic);

            if (connection_state == ConnectionState.CONNECTED && mosq != null) {
                int mid;
                var rc = mosq.unsubscribe (out mid, topic);
                if (rc != Mosquitto.ERR_SUCCESS) {
                    warning ("MqttClient: unsubscribe '%s' failed: %s", topic, Mosquitto.strerror (rc));
                }
            }
        }

        public void publish (string topic, string payload, int qos = 0, bool retain = false) {
            publish_bytes (topic, payload.data, qos, retain);
        }

        public void publish_bytes (string topic, uint8[] payload, int qos = 0, bool retain = false) {
            if (mosq == null || connection_state != ConnectionState.CONNECTED) {
                warning ("MqttClient: publish to '%s' failed: not connected", topic);
                return;
            }

            int mid;
            var rc = mosq.publish (out mid, topic, payload, qos, retain);
            if (rc != Mosquitto.ERR_SUCCESS) {
                warning ("MqttClient: publish to '%s' failed: %s", topic, Mosquitto.strerror (rc));
            }
        }

        /* ---- GLib main loop integration ---- */

        private void start_loop () {
            if (loop_source_id != 0) return;

            loop_source_id = Timeout.add (50, () => {
                if (mosq == null) return Source.REMOVE;

                var rc = mosq.loop (0, 1);
                if (rc != Mosquitto.ERR_SUCCESS && rc != Mosquitto.ERR_NO_CONN) {
                    /* Connection lost during loop — mosquitto will trigger
                     * the disconnect callback which handles reconnection. */
                }
                return Source.CONTINUE;
            });
        }

        private void stop_loop () {
            if (loop_source_id != 0) {
                Source.remove (loop_source_id);
                loop_source_id = 0;
            }
        }

        /* ---- Reconnection ---- */

        private void schedule_reconnect () {
            cancel_reconnect ();
            set_state (ConnectionState.RECONNECTING);

            reconnect_source_id = Timeout.add (reconnect_delay_ms, () => {
                reconnect_source_id = 0;
                attempt_reconnect ();
                return Source.REMOVE;
            });

            /* Exponential backoff */
            reconnect_delay_ms = uint.min (reconnect_delay_ms * 2, RECONNECT_DELAY_MAX_MS);
        }

        private void cancel_reconnect () {
            if (reconnect_source_id != 0) {
                Source.remove (reconnect_source_id);
                reconnect_source_id = 0;
            }
        }

        private void attempt_reconnect () {
            if (mosq == null || broker_host == null) return;

            var rc = mosq.reconnect ();
            if (rc != Mosquitto.ERR_SUCCESS) {
                warning ("MqttClient: reconnect failed: %s", Mosquitto.strerror (rc));
                schedule_reconnect ();
                return;
            }

            /* Ensure loop is pumping */
            start_loop ();
        }

        /* ---- Resubscribe all tracked topics ---- */

        private void resubscribe_all () {
            if (mosq == null) return;

            subscriptions.foreach ((topic, qos) => {
                int mid;
                var rc = mosq.subscribe (out mid, topic, qos);
                if (rc != Mosquitto.ERR_SUCCESS) {
                    warning ("MqttClient: resubscribe '%s' failed: %s",
                             topic, Mosquitto.strerror (rc));
                }
            });
        }

        /* ---- State management ---- */

        private void set_state (ConnectionState new_state) {
            if (connection_state == new_state) return;
            connection_state = new_state;
            connection_changed (new_state);
        }

        /* ---- Static C callbacks (no closure support) ---- */

        private static void on_connect_cb (Mosquitto.Client mosq, void* userdata, int rc) {
            /* Marshal to GLib main thread */
            Idle.add (() => {
                if (_instance == null) return Source.REMOVE;

                if (rc == 0) {
                    _instance.reconnect_delay_ms = RECONNECT_DELAY_INITIAL_MS;
                    _instance.set_state (ConnectionState.CONNECTED);
                    _instance.resubscribe_all ();
                } else {
                    warning ("MqttClient: connection refused: %s",
                             Mosquitto.connack_string (rc));
                    _instance.schedule_reconnect ();
                }
                return Source.REMOVE;
            });
        }

        private static void on_disconnect_cb (Mosquitto.Client mosq, void* userdata, int rc) {
            Idle.add (() => {
                if (_instance == null) return Source.REMOVE;

                if (rc == 0) {
                    /* Clean disconnect requested by us */
                    _instance.set_state (ConnectionState.DISCONNECTED);
                } else {
                    /* Unexpected disconnection — reconnect */
                    warning ("MqttClient: unexpected disconnect: %s",
                             Mosquitto.strerror (rc));
                    _instance.schedule_reconnect ();
                }
                return Source.REMOVE;
            });
        }

        private static void on_message_cb (Mosquitto.Client mosq, void* userdata,
                                            Mosquitto.Message msg) {
            /* Copy data out of the callback context — the Message struct is
             * only valid for the duration of this callback. */
            string topic = msg.topic;
            uint8[] payload = new uint8[msg.payload.length];
            Memory.copy (payload, msg.payload, msg.payload.length);

            Idle.add (() => {
                if (_instance == null) return Source.REMOVE;
                _instance.message_received (topic, payload);
                return Source.REMOVE;
            });
        }
    }
}
