/* toaster.vala — Mu.Toaster
 * App-wide toast dispatch. Views and services raise user-visible
 * notifications without holding a reference to the window's overlay.
 */

namespace Mu {

    public class Toaster : GLib.Object {

        private static Toaster? _instance = null;

        public static Toaster get_default () {
            if (_instance == null) {
                _instance = new Toaster ();
            }
            return _instance;
        }

        public signal void toast_requested (string message);

        /**
         * Show a transient toast in the main window.
         * Safe to call before the window exists (the request is dropped).
         */
        public static void show (string message) {
            get_default ().toast_requested (message);
        }
    }
}
