/* library_repository.vala — Queries library nodes for browsing, search, and metadata resolution. */

namespace Mu {

    public class LibraryRepository : Object {
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;

        /* In-memory metadata cache: item_id -> Json.Object */
        private HashTable<string, Json.Object> metadata_cache;

        private const int BATCH_SIZE = 20;

        public LibraryRepository (CommandCorrelator correlator, LeaseManager lease_mgr) {
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
            this.metadata_cache = new HashTable<string, Json.Object> (str_hash, str_equal);
        }

        /**
         * Browse a container with pagination.
         * Returns the items array from the reply, or null on failure.
         */
        public async Json.Array? browse (string library_id, string container_id,
                                          int64 start, int64 count) {
            var body = LibraryBodies.browse (container_id, start, count);
            var reply = yield correlator.send (library_id, "library.browse", body);

            if (reply == null || !reply.ok || reply.body == null) return null;

            if (reply.body.has_member ("items") &&
                !reply.body.get_null_member ("items")) {
                var items = reply.body.get_array_member ("items");
                cache_items_from_array (items);
                return items;
            }

            return null;
        }

        /**
         * Full-text search across a library.
         * Returns the items array from the reply, or null on failure.
         */
        public async Json.Array? search (string library_id, string query,
                                          int64 start, int64 count) {
            var body = LibraryBodies.search (query, start, count);
            var reply = yield correlator.send (library_id, "library.search", body);

            if (reply == null || !reply.ok || reply.body == null) return null;

            if (reply.body.has_member ("items") &&
                !reply.body.get_null_member ("items")) {
                var items = reply.body.get_array_member ("items");
                cache_items_from_array (items);
                return items;
            }

            return null;
        }

        /**
         * Resolve a single item's metadata and sources.
         * Returns the full reply object, or null on failure.
         */
        public async Json.Object? resolve (string library_id, string item_id,
                                            bool metadata_only = false) {
            var body = LibraryBodies.resolve (item_id, metadata_only);
            var reply = yield correlator.send (library_id, "library.resolve", body);

            if (reply == null || !reply.ok || reply.body == null) return null;

            /* Cache metadata if present */
            if (reply.body.has_member ("metadata") &&
                !reply.body.get_null_member ("metadata")) {
                metadata_cache.set (item_id, reply.body.get_object_member ("metadata"));
            }

            return reply.body;
        }

        /**
         * Resolve multiple items in batches of 20.
         * Returns collected result objects, or null on failure.
         */
        public async GenericArray<Json.Object>? resolve_batch (string library_id,
                                                                string[] item_ids,
                                                                bool metadata_only = false) {
            var results = new GenericArray<Json.Object> ();

            /* Process in chunks of BATCH_SIZE */
            int offset = 0;
            while (offset < item_ids.length) {
                int chunk_end = int.min (offset + BATCH_SIZE, item_ids.length);
                var chunk = new string[chunk_end - offset];
                for (int i = 0; i < chunk.length; i++) {
                    chunk[i] = item_ids[offset + i];
                }

                var body = LibraryBodies.resolve_batch (chunk, metadata_only);
                var reply = yield correlator.send (
                    library_id, "library.resolveBatch", body
                );

                if (reply == null || !reply.ok || reply.body == null) {
                    /* Partial failure — return what we have so far, or null if nothing */
                    return results.length > 0 ? results : null;
                }

                if (reply.body.has_member ("items") &&
                    !reply.body.get_null_member ("items")) {
                    var items = reply.body.get_array_member ("items");
                    for (uint i = 0; i < items.get_length (); i++) {
                        var item = items.get_object_element (i);
                        results.add (item);

                        /* Cache metadata for each resolved item */
                        if (item.has_member ("itemId") && item.has_member ("metadata") &&
                            !item.get_null_member ("metadata")) {
                            var iid = item.get_string_member ("itemId");
                            metadata_cache.set (iid, item.get_object_member ("metadata"));
                        }
                    }
                }

                offset = chunk_end;
            }

            return results;
        }

        /**
         * Get cached metadata for an item (returns null if not cached).
         */
        public Json.Object? get_cached_metadata (string item_id) {
            return metadata_cache.lookup (item_id);
        }

        /**
         * Store metadata in the cache.
         */
        public void cache_metadata (string item_id, Json.Object metadata) {
            metadata_cache.set (item_id, metadata);
        }

        /* ---- Internal ---- */

        /**
         * Cache metadata from browse/search result items that include it inline.
         */
        private void cache_items_from_array (Json.Array items) {
            for (uint i = 0; i < items.get_length (); i++) {
                var item = items.get_object_element (i);
                if (item.has_member ("itemId") && item.has_member ("metadata") &&
                    !item.get_null_member ("metadata")) {
                    var item_id = item.get_string_member ("itemId");
                    metadata_cache.set (item_id, item.get_object_member ("metadata"));
                }
            }
        }
    }
}
