/* lease_manager.vala — Session lease lifecycle management.
 * Acquires, renews, and releases renderer session leases.
 */

namespace Mu {

    private class CachedLease {
        public string session_id;
        public string token;
        public int64 expires_at;  /* unix milliseconds */

        public CachedLease (string session_id, string token, int64 expires_at) {
            this.session_id = session_id;
            this.token = token;
            this.expires_at = expires_at;
        }

        public Lease to_lease () {
            return Lease () {
                session_id = this.session_id,
                token = this.token
            };
        }
    }

    public class LeaseManager : Object {
        private CommandCorrelator correlator;
        private HashTable<string, CachedLease> leases;
        private uint renewal_source_id = 0;

        private const int64 TTL_MS = 300000;            /* 5 minutes */
        private const int64 RENEWAL_THRESHOLD = 60000;   /* renew if expiring within 60s */
        private const int64 NEAR_EXPIRY = 30000;         /* re-acquire if within 30s */

        public LeaseManager (CommandCorrelator correlator) {
            this.correlator = correlator;
            this.leases = new HashTable<string, CachedLease> (str_hash, str_equal);
        }

        /**
         * Get a valid lease for the given renderer, auto-acquiring or renewing as needed.
         * Returns null if the lease could not be obtained.
         */
        public async Lease? ensure_lease (string renderer_id) {
            var now_ms = GLib.get_real_time () / 1000;  /* microseconds → milliseconds */
            var cached = leases.lookup (renderer_id);

            if (cached != null) {
                var remaining = cached.expires_at - now_ms;

                if (remaining > NEAR_EXPIRY) {
                    /* Still valid — renew proactively if within threshold */
                    if (remaining <= RENEWAL_THRESHOLD) {
                        yield renew_lease (renderer_id, cached);
                    }
                    return cached.to_lease ();
                }

                /* Near expiry or expired — re-acquire */
                leases.remove (renderer_id);
            }

            return yield acquire_lease (renderer_id);
        }

        /**
         * Release the lease for a specific renderer.
         *
         * If we don't currently hold a cached lease for the renderer (e.g.
         * another client owns the session, or this lease was acquired by a
         * previous app run), acquire one first so we have valid credentials
         * to authenticate the session.release call. Mirrors Android
         * LeaseManager.releaseLease (LeaseManager.kt:115).
         */
        public async void release_lease (string renderer_id) {
            var cached = leases.lookup (renderer_id);

            if (cached == null) {
                var acquired = yield acquire_lease (renderer_id);
                if (acquired == null) {
                    warning ("LeaseManager: could not acquire lease to release for %s",
                        renderer_id);
                    return;
                }
                cached = leases.lookup (renderer_id);
                if (cached == null) return;
            }

            var lease = cached.to_lease ();
            leases.remove (renderer_id);

            yield correlator.send (
                renderer_id,
                "session.release",
                new Json.Object (),
                lease,
                -1,
                2000
            );
        }

        /**
         * Release all leases (cleanup on shutdown).
         */
        public void release_all () {
            stop_renewal ();

            leases.foreach ((renderer_id, cached) => {
                var lease = cached.to_lease ();
                /* Fire and forget — we are shutting down */
                correlator.send_fire_and_forget (
                    renderer_id,
                    "session.release",
                    new Json.Object (),
                    lease
                );
            });

            leases.remove_all ();
        }

        /**
         * Start the background renewal loop (30s interval).
         */
        public void start_renewal () {
            if (renewal_source_id != 0) return;

            renewal_source_id = Timeout.add_seconds (30, () => {
                check_renewals.begin ();
                return Source.CONTINUE;
            });
        }

        /**
         * Stop the background renewal loop.
         */
        public void stop_renewal () {
            if (renewal_source_id != 0) {
                Source.remove (renewal_source_id);
                renewal_source_id = 0;
            }
        }

        /* ---- Internal ---- */

        private async Lease? acquire_lease (string renderer_id) {
            var body = SessionBodies.acquire (TTL_MS);
            var reply = yield correlator.send (
                renderer_id,
                "session.acquire",
                body,
                null,
                -1,
                5000
            );

            if (reply == null || !reply.ok || reply.body == null) {
                warning ("LeaseManager: failed to acquire lease for %s", renderer_id);
                return null;
            }

            var cached = parse_session_reply (reply.body);
            if (cached == null) {
                warning ("LeaseManager: malformed session reply for %s", renderer_id);
                return null;
            }

            leases.set (renderer_id, cached);
            return cached.to_lease ();
        }

        private async void renew_lease (string renderer_id, CachedLease cached) {
            var lease = cached.to_lease ();
            var body = SessionBodies.renew (TTL_MS);
            var reply = yield correlator.send (
                renderer_id,
                "session.renew",
                body,
                lease,
                -1,
                2000
            );

            if (reply == null) return;

            if (!reply.ok) {
                /* LEASE_MISMATCH means our lease is stale — re-acquire */
                if (reply.err != null && reply.err.code == "LEASE_MISMATCH") {
                    leases.remove (renderer_id);
                    yield acquire_lease (renderer_id);
                }
                return;
            }

            /* Update cached expiry from reply */
            if (reply.body != null) {
                var renewed = parse_session_reply (reply.body);
                if (renewed != null) {
                    leases.set (renderer_id, renewed);
                }
            }
        }

        private async void check_renewals () {
            var now_ms = GLib.get_real_time () / 1000;
            var to_renew = new GenericArray<string> ();

            leases.foreach ((renderer_id, cached) => {
                var remaining = cached.expires_at - now_ms;
                if (remaining > 0 && remaining <= RENEWAL_THRESHOLD) {
                    to_renew.add (renderer_id);
                }
            });

            for (uint i = 0; i < to_renew.length; i++) {
                var renderer_id = to_renew[i];
                var cached = leases.lookup (renderer_id);
                if (cached != null) {
                    yield renew_lease (renderer_id, cached);
                }
            }
        }

        /**
         * Parse the session object from a reply body.
         * Expected: {"session": {"id": "...", "token": "...", "leaseExpiresAt": 123...}}
         */
        private CachedLease? parse_session_reply (Json.Object body) {
            if (!body.has_member ("session")) return null;

            var session = body.get_object_member ("session");
            if (session == null) return null;

            var session_id = session.has_member ("id")
                ? session.get_string_member ("id") : "";
            var token = session.has_member ("token")
                ? session.get_string_member ("token") : "";
            var expires_at = session.has_member ("leaseExpiresAt")
                ? session.get_int_member ("leaseExpiresAt") : (int64) 0;

            if (session_id.length == 0 || token.length == 0) return null;

            return new CachedLease (session_id, token, expires_at);
        }
    }
}
