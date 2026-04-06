/* hires_badge.vala — Small label showing audio format info in uppercase engraved style. */

namespace Mu {

    public class HiResBadge : Gtk.Box {
        private Gtk.Label label;

        public HiResBadge () {
            Object (orientation: Gtk.Orientation.HORIZONTAL, spacing: 0);
            add_css_class ("hires-badge");
            visible = false;

            label = new Gtk.Label ("");
            label.halign = Gtk.Align.CENTER;
            append (label);
        }

        /**
         * Update the badge from current item metadata.
         * Shows format info like "FLAC  24-BIT  96KHZ".
         * Hides itself if no relevant fields are present.
         */
        public void update_from_metadata (Json.Object? metadata) {
            if (metadata == null) {
                visible = false;
                return;
            }

            var parts = new GenericArray<string> ();

            if (metadata.has_member ("format")) {
                var fmt = metadata.get_string_member ("format");
                if (fmt.length > 0) {
                    parts.add (fmt.up ());
                }
            }

            if (metadata.has_member ("bitDepth")) {
                var bd = metadata.get_int_member ("bitDepth");
                if (bd > 0) {
                    parts.add ("%lld-BIT".printf (bd));
                }
            }

            if (metadata.has_member ("sampleRate")) {
                var sr = metadata.get_int_member ("sampleRate");
                if (sr > 0) {
                    if (sr >= 1000) {
                        /* Show as kHz — e.g. 96KHZ, 44.1KHZ */
                        var khz = sr / 1000.0;
                        if (sr % 1000 == 0) {
                            parts.add ("%.0fKHZ".printf (khz));
                        } else {
                            parts.add ("%.1fKHZ".printf (khz));
                        }
                    } else {
                        parts.add ("%lldHZ".printf (sr));
                    }
                }
            }

            if (parts.length == 0) {
                visible = false;
                return;
            }

            /* Join with middle dot separator */
            var sb = new StringBuilder ();
            for (uint i = 0; i < parts.length; i++) {
                if (i > 0) sb.append (" \u00B7 ");
                sb.append (parts[i]);
            }

            label.label = sb.str;
            visible = true;
        }
    }
}
