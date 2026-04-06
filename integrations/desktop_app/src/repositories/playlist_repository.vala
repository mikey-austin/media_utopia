/* playlist_repository.vala — Queries playlist servers for listing, fetching, and loading playlists. */

namespace Mu {

    public class PlaylistRepository : Object {
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;

        public PlaylistRepository (CommandCorrelator correlator, LeaseManager lease_mgr) {
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
        }

        /**
         * List all playlists from a server.
         * Returns parsed PlaylistSummary objects, or null on failure.
         */
        public async GenericArray<PlaylistSummary>? list_playlists (string server_id,
                                                                     string owner = "") {
            var body = PlaylistBodies.list (owner);
            var reply = yield correlator.send (server_id, "playlist.list", body);

            if (reply == null || !reply.ok || reply.body == null) return null;

            var parsed = PlaylistListReply.from_json (reply.body);
            return parsed.playlists;
        }

        /**
         * Get playlist contents (entries array).
         * Returns the entries Json.Array, or null on failure.
         */
        public async Json.Array? get_playlist (string server_id, string playlist_id) {
            var body = PlaylistBodies.get_playlist (playlist_id);
            var reply = yield correlator.send (server_id, "playlist.get", body);

            if (reply == null || !reply.ok || reply.body == null) return null;

            if (reply.body.has_member ("entries") &&
                !reply.body.get_null_member ("entries")) {
                return reply.body.get_array_member ("entries");
            }

            return null;
        }

        /**
         * Load a playlist onto a renderer's queue.
         * Acquires a lease for the renderer, then sends queue.loadPlaylist.
         * Returns true on success.
         */
        public async bool load_playlist (string renderer_id, string server_id,
                                          string playlist_id, string mode = "replace") {
            var lease = yield lease_mgr.ensure_lease (renderer_id);
            if (lease == null) {
                warning ("PlaylistRepository: could not obtain lease for %s", renderer_id);
                return false;
            }

            var body = QueueBodies.load_playlist (server_id, playlist_id, mode);
            var reply = yield correlator.send (renderer_id, "queue.loadPlaylist", body, lease);

            return reply != null && reply.ok;
        }
    }
}
