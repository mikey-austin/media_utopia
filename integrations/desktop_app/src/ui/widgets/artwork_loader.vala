/* artwork_loader.vala — Mu.ArtworkLoader
 * Shared async artwork loading utility with in-memory cache.
 * Fetches images via HTTP (libsoup) and converts to Gdk.Texture.
 */

namespace Mu {

    public delegate void ArtworkCallback (Gdk.Texture? texture);

    public class ArtworkLoader : GLib.Object {

        /* Cache cap: textures are decoded full-size, so keep a bounded LRU
         * instead of growing for every artwork ever browsed. */
        private const uint MAX_CACHE_ENTRIES = 200;

        private Soup.Session session;
        private HashTable<string, Gdk.Texture> cache;

        /* LRU order: most-recently-used URLs at the tail */
        private GLib.Queue<string> lru_order;

        /* Track in-flight requests to avoid duplicates */
        private HashTable<string, bool> inflight;

        /* Pending callbacks for in-flight URLs */
        private HashTable<string, GenericArray<ArtworkCallbackWrapper>> pending;

        public ArtworkLoader () {
            Object ();
            session = new Soup.Session ();
            session.timeout = 15;
            cache = new HashTable<string, Gdk.Texture> (str_hash, str_equal);
            lru_order = new GLib.Queue<string> ();
            inflight = new HashTable<string, bool> (str_hash, str_equal);
            pending = new HashTable<string, GenericArray<ArtworkCallbackWrapper>> (str_hash, str_equal);
        }

        /**
         * Load artwork from URL asynchronously.
         * Calls callback on the main thread with the loaded texture, or null on error.
         * Results are cached by URL.
         */
        public void load_async (string url, owned ArtworkCallback callback) {
            if (url.length == 0) {
                callback (null);
                return;
            }

            /* Cache hit */
            var cached = cache.lookup (url);
            if (cached != null) {
                touch_lru (url);
                callback (cached);
                return;
            }

            /* All remaining paths queue the callback into a wrapper.
             * Hoist construction so the (owned) transfer happens exactly once. */
            var wrapper = new ArtworkCallbackWrapper ((owned) callback);

            /* Already in flight — queue callback */
            if (inflight.contains (url)) {
                var existing = pending.lookup (url);
                if (existing != null) {
                    existing.add (wrapper);
                }
                return;
            }

            /* Mark in-flight */
            inflight.insert (url, true);
            var waiters = new GenericArray<ArtworkCallbackWrapper> ();
            waiters.add (wrapper);
            pending.insert (url, waiters);

            /* Fetch */
            var msg = new Soup.Message ("GET", url);
            if (msg == null) {
                finish_request (url, null);
                return;
            }

            session.send_and_read_async.begin (msg, GLib.Priority.DEFAULT, null,
                (obj, res) => {
                    try {
                        var bytes = session.send_and_read_async.end (res);
                        if (bytes == null || bytes.get_size () == 0) {
                            finish_request (url, null);
                            return;
                        }

                        /* Check HTTP status */
                        if (msg.status_code < 200 || msg.status_code >= 300) {
                            warning ("Artwork fetch failed: HTTP %u for %s",
                                msg.status_code, url);
                            finish_request (url, null);
                            return;
                        }

                        var texture = Gdk.Texture.from_bytes (bytes);
                        insert_cached (url, texture);
                        finish_request (url, texture);

                    } catch (GLib.Error e) {
                        warning ("Artwork load error for %s: %s", url, e.message);
                        finish_request (url, null);
                    }
                }
            );
        }

        /**
         * Complete an in-flight request: invoke all pending callbacks
         * and clean up tracking state.
         */
        private void finish_request (string url, Gdk.Texture? texture) {
            inflight.remove (url);

            var waiters = pending.lookup (url);
            if (waiters != null) {
                for (uint i = 0; i < waiters.length; i++) {
                    waiters[i].invoke (texture);
                }
            }
            pending.remove (url);
        }

        /* ---- LRU bookkeeping ---- */

        private void insert_cached (string url, Gdk.Texture texture) {
            cache.insert (url, texture);
            touch_lru (url);

            while (lru_order.get_length () > MAX_CACHE_ENTRIES) {
                var oldest = lru_order.pop_head ();
                if (oldest != null) {
                    cache.remove (oldest);
                }
            }
        }

        private void touch_lru (string url) {
            lru_order.remove (url);
            lru_order.push_tail (url);
        }

        /**
         * Clear the in-memory cache.
         */
        public void clear () {
            cache.remove_all ();
            lru_order.clear ();
        }
    }

    /**
     * Wrapper to hold an ArtworkCallback in a GenericArray.
     * Delegates cannot be stored directly in GLib containers.
     */
    private class ArtworkCallbackWrapper : GLib.Object {
        private ArtworkCallback cb;

        public ArtworkCallbackWrapper (owned ArtworkCallback callback) {
            Object ();
            this.cb = (owned) callback;
        }

        public void invoke (Gdk.Texture? texture) {
            cb (texture);
        }
    }
}
