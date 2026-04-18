/* library_repository.vala — Queries library nodes for browsing, search, catalog
 * metadata (library.getItem(s)), and playable source resolution
 * (library.resolveSources/Batch).
 */

namespace Mu {

    public class LibraryRepository : Object {
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;

        /* In-memory display cache keyed by "<libraryId>::<itemId>" so the
         * same itemId in different libraries does not collide. */
        private HashTable<string, DisplayMetadata> display_cache;

        private const int BATCH_SIZE = 20;

        public LibraryRepository (CommandCorrelator correlator, LeaseManager lease_mgr) {
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
            this.display_cache = new HashTable<string, DisplayMetadata> (str_hash, str_equal);
        }

        /**
         * Browse a container with pagination.
         * Returns the items array from the reply, or null on failure.
         * Items are { library_ref, display?, attributes? } objects (or container objects).
         */
        public async Json.Array? browse (string library_id, string container_id,
                                          int64 start, int64 count) {
            var body = LibraryBodies.browse (container_id, start, count);
            var reply = yield correlator.send (library_id, "library.browse", body);

            if (reply == null || !reply.ok || reply.body == null) return null;

            if (reply.body.has_member ("items") &&
                !reply.body.get_null_member ("items")) {
                var items = reply.body.get_array_member ("items");
                cache_display_from_items (items);
                return items;
            }

            return null;
        }

        /**
         * Full-text search across a library. Reply shape matches browse.
         */
        public async Json.Array? search (string library_id, string query,
                                          int64 start, int64 count) {
            var body = LibraryBodies.search (query, start, count);
            var reply = yield correlator.send (library_id, "library.search", body);

            if (reply == null || !reply.ok || reply.body == null) return null;

            if (reply.body.has_member ("items") &&
                !reply.body.get_null_member ("items")) {
                var items = reply.body.get_array_member ("items");
                cache_display_from_items (items);
                return items;
            }

            return null;
        }

        /**
         * Fetch catalog metadata for a single library_ref. Returns the parsed reply or null.
         */
        public async LibraryItemReply? get_item (LibraryItemRef library_ref) {
            if (!library_ref.is_valid ()) return null;
            var body = LibraryBodies.get_item (library_ref);
            var reply = yield correlator.send (library_ref.library_id, "library.getItem", body);
            if (reply == null || !reply.ok || reply.body == null) return null;

            var parsed = LibraryItemReply.from_json (reply.body);
            if (parsed.display != null) {
                display_cache.set (cache_key (library_ref), parsed.display);
            }
            return parsed;
        }

        /**
         * Batch-fetch catalog metadata. Refs are grouped by library and dispatched
         * to each library node, with results keyed by "<libraryId>::<itemId>".
         */
        public async HashTable<string, LibraryItemReply>? get_items (LibraryItemRef[] refs) {
            var results = new HashTable<string, LibraryItemReply> (str_hash, str_equal);

            var by_library = group_refs_by_library (refs);
            var lib_ids = new GenericArray<string> ();
            by_library.foreach ((k, v) => { lib_ids.add (k); });

            for (uint l = 0; l < lib_ids.length; l++) {
                var lib_id = lib_ids[l];
                var lib_refs = by_library.lookup (lib_id);
                if (lib_refs == null) continue;

                int offset = 0;
                while (offset < (int) lib_refs.length) {
                    int chunk_end = int.min (offset + BATCH_SIZE, (int) lib_refs.length);
                    var chunk = new LibraryItemRef[chunk_end - offset];
                    for (int i = 0; i < chunk.length; i++) {
                        chunk[i] = lib_refs[offset + i];
                    }

                    var body = LibraryBodies.get_items (chunk);
                    var reply = yield correlator.send (lib_id, "library.getItems", body);
                    if (reply == null || !reply.ok || reply.body == null) {
                        offset = chunk_end;
                        continue;
                    }

                    if (reply.body.has_member ("items") &&
                        !reply.body.get_null_member ("items")) {
                        var arr = reply.body.get_array_member ("items");
                        for (uint i = 0; i < arr.get_length (); i++) {
                            var item = LibraryItemReply.from_json (arr.get_object_element (i));
                            if (item.library_ref == null) continue;
                            var key = cache_key (item.library_ref);
                            results.set (key, item);
                            if (item.display != null) {
                                display_cache.set (key, item.display);
                            }
                        }
                    }

                    offset = chunk_end;
                }
            }

            return results.size () > 0 ? results : null;
        }

        /**
         * Resolve playable sources for a single library_ref.
         */
        public async LibrarySourcesReply? resolve_sources (LibraryItemRef library_ref) {
            if (!library_ref.is_valid ()) return null;
            var body = LibraryBodies.resolve_sources (library_ref);
            var reply = yield correlator.send (
                library_ref.library_id, "library.resolveSources", body);
            if (reply == null || !reply.ok || reply.body == null) return null;
            return LibrarySourcesReply.from_json (reply.body);
        }

        /**
         * Batch-resolve playable sources. Refs are grouped per library; results
         * keyed by "<libraryId>::<itemId>".
         */
        public async HashTable<string, LibrarySourcesReply>? resolve_sources_batch (
            LibraryItemRef[] refs) {

            var results = new HashTable<string, LibrarySourcesReply> (str_hash, str_equal);

            var by_library = group_refs_by_library (refs);
            var lib_ids = new GenericArray<string> ();
            by_library.foreach ((k, v) => { lib_ids.add (k); });

            for (uint l = 0; l < lib_ids.length; l++) {
                var lib_id = lib_ids[l];
                var lib_refs = by_library.lookup (lib_id);
                if (lib_refs == null) continue;

                int offset = 0;
                while (offset < (int) lib_refs.length) {
                    int chunk_end = int.min (offset + BATCH_SIZE, (int) lib_refs.length);
                    var chunk = new LibraryItemRef[chunk_end - offset];
                    for (int i = 0; i < chunk.length; i++) {
                        chunk[i] = lib_refs[offset + i];
                    }

                    var body = LibraryBodies.resolve_sources_batch (chunk);
                    var reply = yield correlator.send (
                        lib_id, "library.resolveSourcesBatch", body);
                    if (reply == null || !reply.ok || reply.body == null) {
                        offset = chunk_end;
                        continue;
                    }

                    if (reply.body.has_member ("items") &&
                        !reply.body.get_null_member ("items")) {
                        var arr = reply.body.get_array_member ("items");
                        for (uint i = 0; i < arr.get_length (); i++) {
                            var item = LibrarySourcesReply.from_json (
                                arr.get_object_element (i));
                            if (item.library_ref == null) continue;
                            results.set (cache_key (item.library_ref), item);
                        }
                    }

                    offset = chunk_end;
                }
            }

            return results.size () > 0 ? results : null;
        }

        /**
         * Get cached display metadata for a library_ref.
         */
        public DisplayMetadata? get_cached_display (LibraryItemRef library_ref) {
            if (!library_ref.is_valid ()) return null;
            return display_cache.lookup (cache_key (library_ref));
        }

        public void cache_display (LibraryItemRef library_ref, DisplayMetadata display) {
            if (!library_ref.is_valid ()) return;
            display_cache.set (cache_key (library_ref), display);
        }

        /* ---- Internal ---- */

        private static string cache_key (LibraryItemRef library_ref) {
            return "%s::%s".printf (library_ref.library_id, library_ref.item_id);
        }

        private HashTable<string, GenericArray<LibraryItemRef>> group_refs_by_library (
            LibraryItemRef[] refs) {
            var grouped = new HashTable<string, GenericArray<LibraryItemRef>> (
                str_hash, str_equal);
            foreach (var r in refs) {
                if (r == null || !r.is_valid ()) continue;
                var bucket = grouped.lookup (r.library_id);
                if (bucket == null) {
                    bucket = new GenericArray<LibraryItemRef> ();
                    grouped.insert (r.library_id, bucket);
                }
                bucket.add (r);
            }
            return grouped;
        }

        /**
         * Cache display metadata from browse/search items that include a library_ref + display.
         */
        private void cache_display_from_items (Json.Array items) {
            for (uint i = 0; i < items.get_length (); i++) {
                var item = items.get_object_element (i);
                LibraryItemRef? library_ref = null;
                if (item.has_member ("ref") && !item.get_null_member ("ref")) {
                    library_ref = LibraryItemRef.from_json (item.get_object_member ("ref"));
                }
                if (library_ref == null || !library_ref.is_valid ()) continue;
                if (item.has_member ("display") && !item.get_null_member ("display")) {
                    var display = DisplayMetadata.from_json (
                        item.get_object_member ("display"));
                    display_cache.set (cache_key (library_ref), display);
                }
            }
        }
    }
}
