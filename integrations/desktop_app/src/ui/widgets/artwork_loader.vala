/* artwork_loader.vala — Mu.ArtworkLoader
 * Shared async artwork loading utility with in-memory cache.
 * Fetches images via HTTP (libsoup) and converts to Gdk.Texture.
 */

namespace Mu {

    public delegate void ArtworkCallback (Gdk.Texture? texture);

    public class ArtworkLoader : GLib.Object {

        private Soup.Session session;
        private HashTable<string, Gdk.Texture> cache;

        /* Track in-flight requests to avoid duplicates */
        private HashTable<string, bool> inflight;

        /* Pending callbacks for in-flight URLs */
        private HashTable<string, GenericArray<ArtworkCallbackWrapper>> pending;

        public ArtworkLoader () {
            Object ();
            session = new Soup.Session ();
            session.timeout = 15;
            cache = new HashTable<string, Gdk.Texture> (str_hash, str_equal);
            inflight = new HashTable<string, bool> (str_hash, str_equal);
            pending = new HashTable<string, GenericArray<ArtworkCallbackWrapper>> (str_hash, str_equal);
        }

        /**
         * Load artwork from URL asynchronously.
         * Calls callback on the main thread with the loaded texture, or null on error.
         * Results are cached by URL.
         */
        public void load_async (string url, ArtworkCallback callback) {
            if (url.length == 0) {
                callback (null);
                return;
            }

            /* Cache hit */
            var cached = cache.lookup (url);
            if (cached != null) {
                callback (cached);
                return;
            }

            /* Already in flight — queue callback */
            if (inflight.contains (url)) {
                var waiters = pending.lookup (url);
                if (waiters != null) {
                    waiters.add (new ArtworkCallbackWrapper (callback));
                }
                return;
            }

            /* Mark in-flight */
            inflight.insert (url, true);
            var waiters = new GenericArray<ArtworkCallbackWrapper> ();
            waiters.add (new ArtworkCallbackWrapper (callback));
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
                        cache.insert (url, texture);
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

        /**
         * Clear the in-memory cache.
         */
        public void clear () {
            cache.remove_all ();
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
