/*
 * MQTT client wrapper around libmosquitto with GLib main-loop marshaling.
 *
 * All Mosquitto callbacks fire on the network thread; every signal emitted
 * here is dispatched via GLib.Idle.add so subscribers can safely touch GTK
 * and RhythmDB.
 */

namespace Mu {

    public delegate void ReplyHandler (ReplyEnvelope reply);

    /* Wrapper to allow storing delegates in a HashTable. */
    private class ReplyHandlerBox : Object {
        public ReplyHandler handler;
        public ReplyHandlerBox (owned ReplyHandler h) { handler = (owned) h; }
    }

    public class MqttClient : Object {

        public signal void connected ();
        public signal void disconnected ();
        public signal void message_received (string topic, uint8[] payload);

        private Mosquitto.Client? mosq = null;
        private string client_id;
        private string host;
        private int port;
        private string? username;
        private string? password;
        private bool running = false;

        /* Pending reply handlers keyed by command ID. */
        private HashTable<string, ReplyHandlerBox> reply_handlers;
        /* Timeout source IDs for reply timeouts. */
        private HashTable<string, uint> reply_timeouts;

        /* Topics to re-subscribe on reconnect. */
        private GenericArray<string> subscriptions;

        public MqttClient (string client_id, string host, int port,
                           string? username = null, string? password = null) {
            this.client_id = client_id;
            this.host = host;
            this.port = port;
            this.username = username;
            this.password = password;

            reply_handlers = new HashTable<string, ReplyHandlerBox> (str_hash, str_equal);
            reply_timeouts = new HashTable<string, uint> (str_hash, str_equal);
            subscriptions = new GenericArray<string> ();
        }

        ~MqttClient () {
            stop ();
        }

        public void start () {
            if (running) return;

            Mosquitto.lib_init ();
            mosq = new Mosquitto.Client (client_id, true);
            MqttClientRegistry.register_client (mosq, this);

            if (username != null && username.length > 0) {
                mosq.username_pw_set (username, password);
            }

            mosq.reconnect_delay_set (1, 30, true);

            /* Callbacks — these fire on the Mosquitto network thread. */
            mosq.connect_callback_set (on_connect_cb);
            mosq.disconnect_callback_set (on_disconnect_cb);
            mosq.message_callback_set (on_message_cb);

            mosq.connect_async (host, port, 60);
            mosq.loop_start ();
            running = true;
        }

        public void stop () {
            if (!running || mosq == null) return;
            running = false;
            mosq.disconnect ();
            mosq.loop_stop (true);
            MqttClientRegistry.unregister_client (mosq);
            mosq = null;
            Mosquitto.lib_cleanup ();
        }

        public void set_will (string topic, uint8[] payload, int qos = 1, bool retain = true) {
            if (mosq == null) return;
            mosq.will_set (topic, payload.length, payload, qos, retain);
        }

        public void subscribe (string topic, int qos = 1) {
            subscriptions.add (topic);
            if (mosq == null) return;
            int mid;
            mosq.subscribe (out mid, topic, qos);
        }

        public void unsubscribe (string topic) {
            /* Remove from re-subscribe list. */
            for (int i = 0; i < subscriptions.length; i++) {
                if (subscriptions[i] == topic) {
                    subscriptions.remove_index (i);
                    break;
                }
            }
            if (mosq == null) return;
            int mid;
            mosq.unsubscribe (out mid, topic);
        }

        public void publish (string topic, uint8[] payload, int qos = 0, bool retain = false) {
            if (mosq == null) return;
            int mid;
            mosq.publish (out mid, topic, payload.length, payload, qos, retain);
        }

        public void publish_string (string topic, string data, int qos = 0, bool retain = false) {
            publish (topic, data.data, qos, retain);
        }

        /**
         * Send a command and await the reply with a timeout.
         * The callback fires on the GLib main thread.
         * Returns false if the command timed out without a reply.
         */
        public void send_command (string target_topic, CommandEnvelope cmd,
                                  owned ReplyHandler on_reply, int timeout_ms = 5000) {
            /* Register reply handler before publishing. */
            string cmd_id = cmd.id;
            reply_handlers.insert (cmd_id, new ReplyHandlerBox ((owned) on_reply));

            /* Set up timeout. */
            var src = GLib.Timeout.add (timeout_ms, () => {
                reply_timeouts.remove (cmd_id);
                var box = reply_handlers.lookup (cmd_id);
                if (box != null) {
                    reply_handlers.remove (cmd_id);
                    var err_reply = new ReplyEnvelope ();
                    err_reply.id = cmd_id;
                    err_reply.reply_type = "error";
                    err_reply.ok = false;
                    err_reply.ts = GLib.get_real_time () / 1000000;
                    err_reply.err = new ReplyError ();
                    err_reply.err.code = "TIMEOUT";
                    err_reply.err.message = "Reply timed out";
                    box.handler (err_reply);
                }
                return Source.REMOVE;
            });
            reply_timeouts.insert (cmd_id, src);

            string json = cmd.to_json_string ();
            message ("MU MQTT: >>> %s to %s (id=%s)", cmd.cmd_type, target_topic, cmd_id);
            publish_string (target_topic, json);
        }

        /**
         * Build and send a command envelope with common fields filled in.
         */
        public CommandEnvelope build_and_send (string target_topic, string cmd_type,
                                               Json.Node body, string from_id,
                                               string reply_topic,
                                               Lease? lease = null,
                                               owned ReplyHandler? on_reply = null,
                                               int timeout_ms = 5000) {
            var cmd = new CommandEnvelope ();
            cmd.id = GLib.Uuid.string_random ();
            cmd.cmd_type = cmd_type;
            cmd.ts = GLib.get_real_time () / 1000000;
            cmd.from = from_id;
            cmd.reply_to = reply_topic;
            cmd.lease = lease;
            cmd.body = body;

            if (on_reply != null) {
                send_command (target_topic, cmd, (owned) on_reply, timeout_ms);
            } else {
                publish_string (target_topic, cmd.to_json_string ());
            }
            return cmd;
        }

        /* ── Mosquitto callbacks (network thread) ───────────── */

        private static void on_connect_cb (Mosquitto.Client mosq, void* userdata, int rc) {
            /* Mosquitto callbacks are static C functions — we use a global
             * registry to map the Mosquitto.Client pointer back to our
             * MqttClient instance. */
            var self = MqttClientRegistry.lookup_client (mosq);
            if (self == null) return;

            GLib.Idle.add (() => {
                /* Re-subscribe all tracked topics. */
                for (int i = 0; i < self.subscriptions.length; i++) {
                    int mid;
                    if (self.mosq != null)
                        self.mosq.subscribe (out mid, self.subscriptions[i], 1);
                }
                self.connected ();
                return Source.REMOVE;
            });
        }

        private static void on_disconnect_cb (Mosquitto.Client mosq, void* userdata, int rc) {
            var self = MqttClientRegistry.lookup_client (mosq);
            if (self == null) return;

            GLib.Idle.add (() => {
                self.disconnected ();
                return Source.REMOVE;
            });
        }

        private static void on_message_cb (Mosquitto.Client mosq, void* userdata, Mosquitto.Message msg) {
            var self = MqttClientRegistry.lookup_client (mosq);
            if (self == null) return;

            /* Copy data off the callback — msg is only valid during this call.
             * Add a null terminator so the payload can be safely cast to string
             * for JSON parsing. */
            string topic = msg.topic;
            uint8[] payload = new uint8[msg.payloadlen + 1];
            GLib.Memory.copy (payload, msg.payload, msg.payloadlen);
            payload[msg.payloadlen] = 0;

            GLib.Idle.add (() => {
                /* Check if this is a reply to a pending command. */
                self.try_dispatch_reply (topic, payload);
                self.message_received (topic, payload);
                return Source.REMOVE;
            });
        }

        private void try_dispatch_reply (string topic, uint8[] payload) {
            /* Replies are JSON with an "id" field matching the command. */
            var reply = ReplyEnvelope.from_json_string ((string) payload);
            if (reply == null || reply.id.length == 0) return;

            /* Only dispatch actual replies (type=ack or type=error), not
             * messages that happen to have an "id" field but echo the
             * command type. */
            if (reply.reply_type != "ack" && reply.reply_type != "error") return;

            string err_info = "";
            if (reply.err != null)
                err_info = @" err=$(reply.err.code): $(reply.err.message)";
            message ("MU MQTT: <<< reply id=%s type=%s ok=%s%s",
                reply.id, reply.reply_type, reply.ok.to_string (), err_info);

            var box = reply_handlers.lookup (reply.id);
            if (box == null) {
                message ("MU MQTT: no handler for reply id=%s (already timed out?)", reply.id);
                return;
            }

            reply_handlers.remove (reply.id);
            /* Cancel the timeout. */
            uint timeout_src;
            if (reply_timeouts.lookup_extended (reply.id, null, out timeout_src)) {
                Source.remove (timeout_src);
                reply_timeouts.remove (reply.id);
            }

            box.handler (reply);
        }
    }

    /*
     * Since Mosquitto callbacks are static C functions without closure userdata
     * support in the Vala binding, we maintain a simple global registry mapping
     * Mosquitto.Client pointers to our MqttClient instances.
     */
    namespace MqttClientRegistry {
        private static HashTable<void*, weak MqttClient>? registry = null;

        public static void register_client (Mosquitto.Client mosq, MqttClient client) {
            if (registry == null)
                registry = new HashTable<void*, weak MqttClient> (direct_hash, direct_equal);
            registry.insert ((void*) mosq, client);
        }

        public static void unregister_client (Mosquitto.Client mosq) {
            if (registry != null)
                registry.remove ((void*) mosq);
        }

        public static MqttClient? lookup_client (Mosquitto.Client mosq) {
            if (registry == null) return null;
            return registry.lookup ((void*) mosq);
        }
    }
}
