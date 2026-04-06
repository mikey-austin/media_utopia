/* notifications.vala — Desktop notifications on track change.
 * Uses GLib.Notification via Gtk.Application.send_notification.
 * Debounces rapid skipping and only notifies when the window is not focused.
 */

namespace Mu {

    public class Notifications : Object {

        private Gtk.Application app;
        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_repo;

        /* Tracking for dedup / debounce */
        private string? last_item_id = null;
        private int64 last_notify_time = 0;  /* microseconds (GLib.get_real_time) */

        private const int64 DEBOUNCE_US = 2000000;  /* 2 seconds in microseconds */

        /* Signal handler IDs */
        private ulong state_changed_id = 0;

        public Notifications (Gtk.Application app,
                              RendererStateRepository state_repo,
                              ActiveRendererRepository active_repo) {
            this.app = app;
            this.state_repo = state_repo;
            this.active_repo = active_repo;

            state_changed_id = state_repo.state_changed.connect (on_state_changed);
        }

        public void stop () {
            if (state_changed_id != 0) {
                state_repo.disconnect (state_changed_id);
                state_changed_id = 0;
            }
        }

        /* ---- Internal ---- */

        private void on_state_changed (string node_id, RendererState state) {
            /* Only react to the active renderer */
            if (node_id != active_repo.active_renderer_id) return;

            /* Must have a current item with metadata */
            if (state.current == null || state.current.metadata == null) return;
            if (state.current.item_id.length == 0) return;

            /* Only notify when actually playing */
            if (state.playback == null || state.playback.status != "playing") return;

            /* Track must have changed */
            if (state.current.item_id == last_item_id) return;

            /* Check if the window is focused — skip notification if so */
            var window = app.active_window;
            if (window != null && window.is_active) return;

            /* Debounce: skip if less than 2 seconds since last notification */
            var now = GLib.get_real_time ();
            if ((now - last_notify_time) < DEBOUNCE_US) return;

            /* All checks passed — send notification */
            last_item_id = state.current.item_id;
            last_notify_time = now;

            var meta = state.current.metadata;
            var title = meta.has_member ("title")
                ? meta.get_string_member ("title") : "Unknown Track";
            var artist = meta.has_member ("artist")
                ? meta.get_string_member ("artist") : "";
            var album = meta.has_member ("album")
                ? meta.get_string_member ("album") : "";

            /* Build body: "Artist — Album" or just "Artist" or "Album" */
            string body;
            if (artist.length > 0 && album.length > 0) {
                body = "%s \u2014 %s".printf (artist, album);
            } else if (artist.length > 0) {
                body = artist;
            } else if (album.length > 0) {
                body = album;
            } else {
                body = "Now Playing";
            }

            var notification = new GLib.Notification (title);
            notification.set_body (body);
            notification.set_default_action ("app.raise-window");

            /* Action buttons */
            notification.add_button ("Next", "app.next");
            notification.add_button ("Pause", "app.play-pause");

            app.send_notification ("track-change", notification);
        }
    }
}
