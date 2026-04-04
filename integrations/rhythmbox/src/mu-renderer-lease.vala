/*
 * Lease manager for the renderer role — port of renderer_core/lease.go.
 * Manages exclusive session ownership with TTL-based expiration.
 */

namespace Mu {

    public class RendererLease : Object {

        private string? session_id = null;
        private string? token = null;
        private string? owner = null;
        private int64 expiry = 0;  /* Unix seconds */
        private string id_prefix;

        public RendererLease (string id_prefix) {
            this.id_prefix = id_prefix;
        }

        public SessionLease? acquire (string req_owner, int64 ttl_ms) {
            if (is_active ()) return null;  /* Already held */

            session_id = @"mu:session:$(id_prefix):$(rand_token ())";
            token = rand_token ();
            owner = req_owner;
            expiry = (GLib.get_real_time () / 1000000) + (ttl_ms / 1000);

            return current_lease ();
        }

        public SessionLease? renew (string req_session_id, string req_token, int64 ttl_ms) {
            if (!is_active ()) return null;
            if (session_id != req_session_id || token != req_token) return null;

            expiry = (GLib.get_real_time () / 1000000) + (ttl_ms / 1000);
            return current_lease ();
        }

        public bool release (string req_session_id, string req_token) {
            if (!is_active ()) return false;
            if (session_id != req_session_id || token != req_token) return false;

            session_id = null;
            token = null;
            owner = null;
            expiry = 0;
            return true;
        }

        public bool require_lease (string req_session_id, string req_token) {
            if (!is_active ()) return false;
            return (session_id == req_session_id && token == req_token);
        }

        public SessionState? current_state () {
            if (!is_active ()) return null;
            var s = new SessionState ();
            s.id = session_id;
            s.owner = owner;
            s.lease_expires_at = expiry;
            return s;
        }

        public bool is_active () {
            if (session_id == null) return false;
            int64 now = GLib.get_real_time () / 1000000;
            if (now >= expiry) {
                /* Expired — clean up. */
                session_id = null;
                token = null;
                owner = null;
                expiry = 0;
                return false;
            }
            return true;
        }

        private SessionLease current_lease () {
            var sl = new SessionLease ();
            sl.id = session_id;
            sl.token = token;
            sl.owner = owner;
            sl.lease_expires_at = expiry;
            return sl;
        }

        private static string rand_token () {
            uint8 buf[16];
            for (int i = 0; i < 16; i++) {
                buf[i] = (uint8) GLib.Random.int_range (0, 256);
            }
            var sb = new StringBuilder ();
            for (int i = 0; i < 16; i++) {
                sb.append_printf ("%02x", buf[i]);
            }
            return sb.str;
        }
    }
}
