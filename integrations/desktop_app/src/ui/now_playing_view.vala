/* now_playing_view.vala — Stub NowPlayingView : Adw.Bin */

namespace Mu {

    public class NowPlayingView : Adw.Bin {

        construct {
            add_css_class ("now-playing-view");

            var box = new Gtk.Box (Gtk.Orientation.VERTICAL, 16);
            box.halign = Gtk.Align.CENTER;
            box.valign = Gtk.Align.CENTER;
            box.hexpand = true;
            box.vexpand = true;

            var icon = new Gtk.Image.from_resource ("/com/mediautopia/desktop/icons/mu-motif.svg");
            icon.pixel_size = 120;
            icon.margin_bottom = 16;
            box.append (icon);

            var title = new Gtk.Label ("Now Playing");
            title.add_css_class ("heading-large");
            box.append (title);

            var subtitle = new Gtk.Label ("No track playing");
            subtitle.add_css_class ("text-secondary");
            box.append (subtitle);

            this.child = box;
        }
    }
}
