/*
 * MU canonical queue — port of renderer_core/queue.go.
 * Maintains the queue state with revision counter and repeat/shuffle modes.
 */

namespace Mu {

    public class QueueEntry : Object {
        public string queue_entry_id { get; set; default = ""; }
        public string item_id        { get; set; default = ""; }
        public HashTable<string, string> metadata { get; owned set; }
        public string? resolved_url  { get; set; default = null; }
        public string? resolved_mime { get; set; default = null; }

        construct {
            metadata = new HashTable<string, string> (str_hash, str_equal);
        }
    }

    public class RendererQueue : Object {

        public int64  revision    { get; private set; default = 0; }
        public int64  index       { get; private set; default = 0; }
        public bool   repeat      { get; private set; default = false; }
        public string repeat_mode { get; private set; default = "off"; }
        public bool   shuffle     { get; private set; default = false; }

        private GenericArray<QueueEntry> entries;

        construct {
            entries = new GenericArray<QueueEntry> ();
        }

        public int length {
            get { return entries.length; }
        }

        public QueueEntry? current () {
            if (entries.length == 0 || index < 0 || index >= entries.length) return null;
            return entries[(int) index];
        }

        public QueueEntry? get_entry (int idx) {
            if (idx < 0 || idx >= entries.length) return null;
            return entries[idx];
        }

        /* ── Mutations ──────────────────────────────────────── */

        public void set_queue (GenericArray<QueueEntry> new_entries, int64 start_index) {
            entries = new_entries;
            index = clamp_index (start_index);
            revision++;
        }

        public void add_entries (GenericArray<QueueEntry> new_entries,
                                 string position = "end", int64 at_index = -1) {
            if (new_entries.length == 0) return;

            int insert_pos;
            switch (position) {
                case "next":
                    insert_pos = (int) (index + 1);
                    break;
                case "at":
                    insert_pos = (int) clamp_index (at_index);
                    break;
                default: /* "end" */
                    insert_pos = entries.length;
                    break;
            }

            if (insert_pos > entries.length) insert_pos = entries.length;

            /* Insert in reverse to preserve order at insert_pos. */
            for (int i = new_entries.length - 1; i >= 0; i--) {
                entries.insert (insert_pos, new_entries[i]);
            }

            /* Adjust current index if items were inserted before it. */
            if (insert_pos <= (int) index) {
                index += new_entries.length;
            }

            revision++;
        }

        public bool remove_by_index (int64 rm_index) {
            if (rm_index < 0 || rm_index >= entries.length) return false;

            entries.remove_index ((uint) rm_index);

            if (rm_index < index) {
                index--;
            } else if (rm_index == index && index >= entries.length) {
                index = int64.max (0, entries.length - 1);
            }

            revision++;
            return true;
        }

        public bool remove_by_id (string queue_entry_id) {
            for (int i = 0; i < entries.length; i++) {
                if (entries[i].queue_entry_id == queue_entry_id) {
                    return remove_by_index (i);
                }
            }
            return false;
        }

        public bool move_entry (int64 from_idx, int64 to_idx) {
            if (from_idx < 0 || from_idx >= entries.length) return false;
            if (to_idx < 0 || to_idx >= entries.length) return false;
            if (from_idx == to_idx) return true;

            var entry = entries[(int) from_idx];
            entries.remove_index ((uint) from_idx);
            entries.insert ((int) to_idx, entry);

            /* Adjust current index. */
            if (from_idx == index) {
                index = to_idx;
            } else if (from_idx < index && to_idx >= index) {
                index--;
            } else if (from_idx > index && to_idx <= index) {
                index++;
            }

            revision++;
            return true;
        }

        public void jump (int64 new_index) {
            index = clamp_index (new_index);
            revision++;
        }

        public void clear () {
            entries = new GenericArray<QueueEntry> ();
            index = 0;
            revision++;
        }

        public void shuffle_queue (int64 seed) {
            if (entries.length <= 1) return;

            /* Keep current entry, shuffle the rest. */
            var cur = current ();
            var rand = new GLib.Rand.with_seed ((uint32) seed);

            for (int i = entries.length - 1; i > 0; i--) {
                int j = (int) rand.int_range (0, i + 1);
                if (i != j) {
                    var tmp = entries[i];
                    entries[i] = entries[j];
                    entries[j] = tmp;
                }
            }

            /* Restore current entry to index 0. */
            if (cur != null) {
                for (int i = 0; i < entries.length; i++) {
                    if (entries[i].queue_entry_id == cur.queue_entry_id) {
                        if (i != 0) {
                            var tmp = entries[0];
                            entries[0] = entries[i];
                            entries[i] = tmp;
                        }
                        break;
                    }
                }
                index = 0;
            }

            revision++;
        }

        public void update_shuffle (bool enabled) {
            shuffle = enabled;
            revision++;
        }

        public void update_repeat (bool enabled, string mode = "off") {
            repeat = enabled;
            repeat_mode = mode;
            revision++;
        }

        /* ── Navigation ─────────────────────────────────────── */

        /**
         * Advance to next track. Returns true if there is a track to play.
         */
        public bool next () {
            if (entries.length == 0) return false;

            if (repeat_mode == "one") {
                return true;  /* Same track */
            }

            if (index + 1 < entries.length) {
                index++;
                return true;
            }

            /* At end of queue. */
            if (repeat || repeat_mode == "all") {
                index = 0;
                return true;
            }

            return false;  /* End of queue, no repeat. */
        }

        /**
         * Go to previous track. Returns true if there is a track to play.
         */
        public bool prev () {
            if (entries.length == 0) return false;

            if (index > 0) {
                index--;
                return true;
            }

            /* At start of queue. */
            if (repeat || repeat_mode == "all") {
                index = entries.length - 1;
                return true;
            }

            return false;
        }

        /* ── Snapshot for queue.get replies ──────────────────── */

        public Json.Node snapshot_json (int64 from, int64 count) {
            int start = (int) clamp_index (from);
            int end = int.min (start + (int) count, entries.length);

            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("revision"); b.add_int_value (revision);
            b.set_member_name ("index");    b.add_int_value (index);
            b.set_member_name ("entries");
            b.begin_array ();
            for (int i = start; i < end; i++) {
                var e = entries[i];
                b.begin_object ();
                b.set_member_name ("queueEntryId"); b.add_string_value (e.queue_entry_id);
                b.set_member_name ("itemId");       b.add_string_value (e.item_id);
                if (e.metadata.size () > 0) {
                    b.set_member_name ("metadata");
                    b.begin_object ();
                    e.metadata.foreach ((k, v) => {
                        b.set_member_name (k); b.add_string_value (v);
                    });
                    b.end_object ();
                }
                b.end_object ();
            }
            b.end_array ();
            b.end_object ();
            return b.get_root ();
        }

        public Mu.QueueState to_state () {
            var qs = new Mu.QueueState ();
            qs.revision = revision;
            qs.length = entries.length;
            qs.index = index;
            qs.repeat = repeat;
            qs.repeat_mode = repeat_mode;
            qs.shuffle = shuffle;
            return qs;
        }

        /* ── Helpers ────────────────────────────────────────── */

        private int64 clamp_index (int64 i) {
            if (entries.length == 0) return 0;
            if (i < 0) return 0;
            if (i >= entries.length) return entries.length - 1;
            return i;
        }
    }
}
