/*
 * Ring-buffer command deduplication — port of pkg/mu/dedup.go.
 * Tracks recently processed command IDs to reject MQTT QoS 1 redeliveries.
 */

namespace Mu {

    public class CommandDedup : Object {

        private string[] ring;
        private int next;
        private GenericSet<string> seen;
        private GLib.Mutex mu;

        public CommandDedup (int size = 128) {
            ring = new string[size];
            next = 0;
            seen = new GenericSet<string> (str_hash, str_equal);
            mu = GLib.Mutex ();
        }

        /**
         * Returns true if this command ID has been seen before (duplicate).
         * If new, records it and returns false.
         */
        public bool is_duplicate (string cmd_id) {
            mu.lock ();
            try {
                if (seen.contains (cmd_id)) {
                    return true;
                }

                /* Evict the oldest entry if the ring is full. */
                string? evicted = ring[next];
                if (evicted != null && evicted.length > 0) {
                    seen.remove (evicted);
                }

                ring[next] = cmd_id;
                seen.add (cmd_id);
                next = (next + 1) % ring.length;

                return false;
            } finally {
                mu.unlock ();
            }
        }
    }
}
