/* command_correlator.vala — Sends commands to MU nodes and correlates replies by ID.
 * Uses the Vala async pattern: send() suspends until a matching reply arrives or timeout.
 */

namespace Mu {

    private class PendingCommand {
        public SourceFunc? callback = null;
        public ReplyEnvelope? reply = null;
        public uint timeout_id = 0;
    }

    public class CommandCorrelator : Object {
        private MqttClient mqtt;
        private string topic_base = MqttTopics.BASE;
        private string controller_id = "";
        private string identity = "";
        private string reply_topic = "";

        private HashTable<string, PendingCommand> pending;
        private ulong message_handler_id = 0;

        public CommandCorrelator (MqttClient mqtt) {
            this.mqtt = mqtt;
            this.pending = new HashTable<string, PendingCommand> (str_hash, str_equal);
        }

        /**
         * Subscribe to the reply topic and wire the message handler.
         */
        public void setup (string topic_base, string controller_id, string identity) {
            this.topic_base = topic_base;
            this.controller_id = controller_id;
            this.identity = identity;
            this.reply_topic = MqttTopics.reply (controller_id, topic_base);

            mqtt.subscribe (reply_topic, 1);
            message_handler_id = mqtt.message_received.connect (on_message);
        }

        /**
         * Unsubscribe and disconnect the message handler.
         */
        public void cleanup () {
            if (message_handler_id != 0) {
                mqtt.disconnect (message_handler_id);
                message_handler_id = 0;
            }

            if (reply_topic.length > 0) {
                mqtt.unsubscribe (reply_topic);
            }

            /* Cancel all pending timeouts */
            pending.foreach ((id, pc) => {
                if (pc.timeout_id != 0) {
                    Source.remove (pc.timeout_id);
                }
            });
            pending.remove_all ();
        }

        /**
         * Send a command and await the reply with timeout.
         * Returns null on timeout.
         */
        public async ReplyEnvelope? send (
            string node_id,
            string cmd_type,
            Json.Object body,
            Lease? lease = null,
            int64 if_revision = -1,
            int timeout_ms = 2000
        ) {
            var cmd_id = GLib.Uuid.string_random ();
            var ts = (int64) (GLib.get_real_time () / 1000000);

            var cmd = new CommandEnvelope (cmd_id, cmd_type, ts, identity, body);
            cmd.reply_to = reply_topic;
            if (lease != null) {
                cmd.lease = lease;
            }
            if (if_revision >= 0) {
                cmd.if_revision = if_revision;
            }

            var pending_cmd = new PendingCommand ();
            pending.insert (cmd_id, pending_cmd);

            /* Publish the command */
            var cmd_topic = MqttTopics.commands (node_id, topic_base);
            mqtt.publish (cmd_topic, cmd.to_json_string (), 1);

            /* Set timeout */
            pending_cmd.timeout_id = Timeout.add ((uint) timeout_ms, () => {
                pending_cmd.timeout_id = 0;
                pending_cmd.reply = null;
                if (pending_cmd.callback != null) {
                    Idle.add ((owned) pending_cmd.callback);
                }
                pending.remove (cmd_id);
                return Source.REMOVE;
            });

            /* Suspend until reply or timeout */
            pending_cmd.callback = send.callback;
            yield;

            return pending_cmd.reply;
        }

        /**
         * Fire and forget — no reply expected.
         */
        public void send_fire_and_forget (
            string node_id,
            string cmd_type,
            Json.Object body,
            Lease? lease = null
        ) {
            var cmd_id = GLib.Uuid.string_random ();
            var ts = (int64) (GLib.get_real_time () / 1000000);

            var cmd = new CommandEnvelope (cmd_id, cmd_type, ts, identity, body);
            if (lease != null) {
                cmd.lease = lease;
            }

            var cmd_topic = MqttTopics.commands (node_id, topic_base);
            mqtt.publish (cmd_topic, cmd.to_json_string (), 1);
        }

        /**
         * Handle incoming MQTT messages — match replies to pending commands.
         */
        private void on_message (string topic, uint8[] payload) {
            if (topic != reply_topic) return;

            ReplyEnvelope? reply = null;
            try {
                reply = ReplyEnvelope.from_json_string (payload_to_string (payload));
            } catch (GLib.Error e) {
                warning ("CommandCorrelator: failed to parse reply: %s", e.message);
                return;
            }

            if (reply == null || reply.id.length == 0) return;

            var pending_cmd = pending.lookup (reply.id);
            if (pending_cmd == null) return;

            /* Cancel timeout */
            if (pending_cmd.timeout_id != 0) {
                Source.remove (pending_cmd.timeout_id);
                pending_cmd.timeout_id = 0;
            }

            pending_cmd.reply = reply;
            if (pending_cmd.callback != null) {
                Idle.add ((owned) pending_cmd.callback);
            }
            pending.remove (reply.id);
        }
    }
}
