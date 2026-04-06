/* command_dedup.vala — Ring buffer for MQTT QoS 1 command deduplication.
 * Prevents double-execution of redelivered messages.
 */

namespace Mu {

    public class CommandDedup {
        private string?[] ring;
        private HashTable<string, bool> set;
        private int next = 0;
        private int capacity;

        public CommandDedup (int capacity = 128) {
            this.capacity = capacity;
            this.ring = new string?[capacity];
            this.set = new HashTable<string, bool> (str_hash, str_equal);
        }

        /**
         * Returns true if this command ID was already seen.
         * Records the ID if it is new.
         */
        public bool seen (string command_id) {
            if (set.contains (command_id)) {
                return true;
            }

            /* Evict the oldest entry if the ring slot is occupied */
            if (ring[next] != null) {
                set.remove (ring[next]);
            }

            ring[next] = command_id;
            set.set (command_id, true);
            next = (next + 1) % capacity;

            return false;
        }
    }

    /**
     * Returns true if this command type should be deduped.
     * Never dedup: session.*, queue.get, library.*, playlist.*, snapshot.*
     */
    public bool should_dedup (string cmd_type) {
        if (cmd_type.has_prefix ("session.")) return false;
        if (cmd_type.has_prefix ("library.")) return false;
        if (cmd_type.has_prefix ("playlist.")) return false;
        if (cmd_type.has_prefix ("snapshot.")) return false;
        if (cmd_type == "queue.get") return false;
        return true;
    }
}
