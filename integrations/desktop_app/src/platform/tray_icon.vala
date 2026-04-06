/* tray_icon.vala — Keep-alive and minimize-to-tray support.
 *
 * GTK4 has no built-in system tray API, and libayatana-appindicator3 is
 * GTK3-only.  Instead we rely on:
 *   1. GLib.Application.hold() to keep the main loop alive when the
 *      window is hidden (close-to-tray setting).
 *   2. MPRIS2 D-Bus interface lets the desktop environment show media
 *      controls (effectively acting as our "tray" presence).
 *   3. The window can be re-raised via MPRIS2 Raise() or by re-launching
 *      the app (single-instance via GApplication).
 *
 * This class manages the hold/release lifecycle so the app doesn't quit
 * when the user closes (hides) the window.
 */

namespace Mu {

    public class TrayManager : Object {

        private Gtk.Application app;
        private GLib.Settings settings;
        private bool holding = false;

        public TrayManager (Gtk.Application app, GLib.Settings settings) {
            this.app = app;
            this.settings = settings;

            /* If close-to-tray is enabled, hold the application so the
             * main loop survives window hide. */
            update_hold_state ();

            /* React to runtime setting changes */
            settings.changed["close-to-tray"].connect (() => {
                update_hold_state ();
            });
        }

        /**
         * Call on shutdown to ensure the hold is released.
         */
        public void stop () {
            if (holding) {
                app.release ();
                holding = false;
            }
        }

        /* ---- Internal ---- */

        private void update_hold_state () {
            var want_hold = settings.get_boolean ("close-to-tray");

            if (want_hold && !holding) {
                app.hold ();
                holding = true;
                message ("TrayManager: app held — window can be hidden without quitting");
            } else if (!want_hold && holding) {
                app.release ();
                holding = false;
                message ("TrayManager: app released — closing window will quit");
            }
        }
    }
}
