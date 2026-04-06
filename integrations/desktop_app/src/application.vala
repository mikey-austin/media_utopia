/* application.vala — Mu.Application : Adw.Application */

namespace Mu {

    public class Application : Adw.Application {

        public Application () {
            Object (
                application_id: "com.mediautopia.desktop",
                flags: ApplicationFlags.DEFAULT_FLAGS
            );
        }

        protected override void startup () {
            base.startup ();
            load_css ();
        }

        protected override void activate () {
            var window = this.active_window;
            if (window == null) {
                window = new Mu.Window (this);
            }
            window.present ();
        }

        private void load_css () {
            var provider = new Gtk.CssProvider ();
            provider.load_from_resource ("/com/mediautopia/desktop/style.css");
            Gtk.StyleContext.add_provider_for_display (
                Gdk.Display.get_default (),
                provider,
                Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION
            );
        }
    }
}
