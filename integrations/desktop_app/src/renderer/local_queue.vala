/* local_queue.vala — In-memory queue state machine for the local GStreamer renderer. */

namespace Mu {

    public class LocalQueueEntry {
        public string queue_entry_id;
        public LibraryItemRef? library_ref;
        public string url;
        public string mime;
        public bool byte_range;
        public DisplayMetadata? display;

        public LocalQueueEntry (LibraryItemRef? library_ref, string url = "", string mime = "",
                                 bool byte_range = false, DisplayMetadata? display = null) {
            this.queue_entry_id = "mu:queueentry:renderer:R:" + GLib.Uuid.string_random ();
            this.library_ref = library_ref;
            this.url = url;
            this.mime = mime;
            this.byte_range = byte_range;
            this.display = display;
        }

        public LocalQueueEntry.with_id (string queue_entry_id, LibraryItemRef? library_ref,
                                         string url = "", string mime = "",
                                         bool byte_range = false,
                                         DisplayMetadata? display = null) {
            this.queue_entry_id = queue_entry_id;
            this.library_ref = library_ref;
            this.url = url;
            this.mime = mime;
            this.byte_range = byte_range;
            this.display = display;
        }

        /**
         * Stable identity for cache lookups, dedup, etc. Returns "" if no library_ref.
         */
        public string identity_key () {
            if (library_ref == null || !library_ref.is_valid ()) return "";
            return "%s::%s".printf (library_ref.library_id, library_ref.item_id);
        }
    }

    public class LocalQueue : Object {
        public GenericArray<LocalQueueEntry> entries;
        public int64 revision { get; private set; default = 0; }
        public int64 index { get; set; default = 0; }
        public bool shuffle { get; set; default = false; }
        public bool repeat { get; set; default = false; }
        public string repeat_mode { get; private set; default = "off"; }

        public LocalQueue () {
            entries = new GenericArray<LocalQueueEntry> ();
        }

        /* ---- Mutations (all bump revision) ---- */

        public void set_entries (int64 start_index, GenericArray<LocalQueueEntry> new_entries) {
            entries = new GenericArray<LocalQueueEntry> ();
            for (uint i = 0; i < new_entries.length; i++) {
                entries.add (new_entries[i]);
            }
            index = start_index;
            clamp_index ();
            bump_revision ();
        }

        public void add (string position, GenericArray<LocalQueueEntry> new_entries,
                          int64 at_index = -1) {
            if (position == "next") {
                var insert_at = (int) (index + 1);
                for (uint i = 0; i < new_entries.length; i++) {
                    if (insert_at <= (int) entries.length) {
                        entries.insert ((int) insert_at + (int) i, new_entries[i]);
                    } else {
                        entries.add (new_entries[i]);
                    }
                }
            } else if (position == "at" && at_index >= 0) {
                var insert_at = int64.min (at_index, (int64) entries.length);
                for (uint i = 0; i < new_entries.length; i++) {
                    entries.insert ((int) insert_at + (int) i, new_entries[i]);
                }
                if (insert_at <= index) {
                    index += (int64) new_entries.length;
                }
            } else {
                for (uint i = 0; i < new_entries.length; i++) {
                    entries.add (new_entries[i]);
                }
            }
            bump_revision ();
        }

        public void remove_entry (string? queue_entry_id, int64 remove_index = -1) {
            int target = -1;

            if (queue_entry_id != null && queue_entry_id.length > 0) {
                for (uint i = 0; i < entries.length; i++) {
                    if (entries[i].queue_entry_id == queue_entry_id) {
                        target = (int) i;
                        break;
                    }
                }
            } else if (remove_index >= 0 && remove_index < (int64) entries.length) {
                target = (int) remove_index;
            }

            if (target < 0) return;

            entries.remove_index ((uint) target);

            if ((int64) target < index) {
                index--;
            } else if ((int64) target == index && index >= (int64) entries.length) {
                index = int64.max (0, (int64) entries.length - 1);
            }

            bump_revision ();
        }

        public void move_entry (int64 from_index, int64 to_index) {
            if (from_index < 0 || from_index >= (int64) entries.length) return;
            if (to_index < 0 || to_index >= (int64) entries.length) return;
            if (from_index == to_index) return;

            var entry = entries[(uint) from_index];
            entries.remove_index ((uint) from_index);
            entries.insert ((int) to_index, entry);

            if (index == from_index) {
                index = to_index;
            } else {
                if (from_index < index && to_index >= index) {
                    index--;
                } else if (from_index > index && to_index <= index) {
                    index++;
                }
            }

            bump_revision ();
        }

        public void clear () {
            entries = new GenericArray<LocalQueueEntry> ();
            index = 0;
            bump_revision ();
        }

        public void jump (int64 jump_index) {
            if (jump_index >= 0 && jump_index < (int64) entries.length) {
                index = jump_index;
                bump_revision ();
            }
        }

        public void shuffle_entries (int64 seed) {
            if (entries.length <= 1) return;

            var current = current_entry ();

            var rng = new Rand.with_seed ((uint32) seed);
            for (int i = (int) entries.length - 1; i > 0; i--) {
                var j = (int) rng.int_range (0, i + 1);
                if (i != j) {
                    var tmp = entries[i];
                    entries[i] = entries[j];
                    entries[j] = tmp;
                }
            }

            if (current != null) {
                for (uint i = 0; i < entries.length; i++) {
                    if (entries[i].queue_entry_id == current.queue_entry_id) {
                        if (i != 0) {
                            var tmp = entries[0];
                            entries[0] = entries[i];
                            entries[i] = tmp;
                        }
                        break;
                    }
                }
            }
            index = 0;

            bump_revision ();
        }

        public void set_shuffle_mode (bool value) {
            shuffle = value;
            bump_revision ();
        }

        public void apply_repeat_mode (string mode) {
            repeat_mode = mode;
            repeat = (mode != "off");
            bump_revision ();
        }

        /* ---- Navigation ---- */

        public LocalQueueEntry? current_entry () {
            if (entries.length == 0) return null;
            if (index < 0 || index >= (int64) entries.length) return null;
            return entries[(uint) index];
        }

        public LocalQueueEntry? next_entry () {
            if (entries.length == 0) return null;

            if (repeat_mode == "one") {
                return current_entry ();
            }

            var next_idx = index + 1;
            if (next_idx >= (int64) entries.length) {
                if (repeat) {
                    index = 0;
                    return current_entry ();
                }
                return null;
            }

            index = next_idx;
            return current_entry ();
        }

        public LocalQueueEntry? prev_entry () {
            if (entries.length == 0) return null;

            var prev_idx = index - 1;
            if (prev_idx < 0) {
                if (repeat) {
                    index = (int64) entries.length - 1;
                    return current_entry ();
                }
                return null;
            }

            index = prev_idx;
            return current_entry ();
        }

        /* ---- Query ---- */

        public QueueState summary () {
            var state = new QueueState ();
            state.revision = revision;
            state.length = (int64) entries.length;
            state.index = index;
            state.repeat = repeat;
            state.repeat_mode = repeat_mode;
            state.shuffle = shuffle;
            return state;
        }

        public QueueGetReply snapshot (int64 from, int64 count) {
            var reply = new QueueGetReply ();
            reply.revision = revision;
            reply.index = index;

            var end = int64.min (from + count, (int64) entries.length);
            for (int64 i = from; i < end; i++) {
                var entry = entries[(uint) i];
                var item = new QueueEntry ();
                item.queue_entry_id = entry.queue_entry_id;
                item.library_ref = entry.library_ref;
                if (entry.url.length > 0) {
                    item.resolved = new ResolvedSource.with_url (
                        entry.url, entry.mime, entry.byte_range);
                }
                item.display = entry.display;
                reply.entries.add (item);
            }

            return reply;
        }

        /* ---- Private ---- */

        private void bump_revision () {
            revision++;
        }

        private void clamp_index () {
            if (entries.length == 0) {
                index = 0;
            } else if (index >= (int64) entries.length) {
                index = (int64) entries.length - 1;
            } else if (index < 0) {
                index = 0;
            }
        }
    }
}
