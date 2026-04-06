/* seek_bar.vala — Seek slider with position interpolation and time labels. */

namespace Mu {

    public class SeekBar : Gtk.Box {
        private Gtk.Scale slider;
        private Gtk.Label current_label;
        private Gtk.Label remaining_label;
        private uint interpolation_id = 0;
        private bool user_dragging = false;

        private int64 _position_ms = 0;
        public int64 position_ms {
            get { return _position_ms; }
            set {
                _position_ms = value;
                if (!user_dragging) {
                    update_slider ();
                    update_labels ();
                }
            }
        }

        private int64 _duration_ms = 0;
        public int64 duration_ms {
            get { return _duration_ms; }
            set {
                _duration_ms = value;
                slider.set_range (0, (double) (_duration_ms > 0 ? _duration_ms : 1));
                if (!user_dragging) {
                    update_slider ();
                    update_labels ();
                }
            }
        }

        private bool _is_playing = false;
        public bool is_playing {
            get { return _is_playing; }
            set {
                _is_playing = value;
                if (_is_playing) {
                    start_interpolation ();
                } else {
                    stop_interpolation ();
                }
            }
        }

        public signal void seek_requested (int64 position_ms);

        public SeekBar () {
            Object (orientation: Gtk.Orientation.VERTICAL, spacing: 4);

            /* Seek slider */
            slider = new Gtk.Scale.with_range (Gtk.Orientation.HORIZONTAL, 0, 1, 1);
            slider.draw_value = false;
            slider.hexpand = true;

            /* Detect user drag start/end via the GtkRange change-value signal */
            slider.change_value.connect ((scroll, new_value) => {
                user_dragging = true;
                /* Update labels while dragging to show seek position */
                _position_ms = (int64) new_value;
                update_labels ();
                return false;  /* allow the value to be set */
            });

            /* When user releases, emit seek_requested */
            var drag_gesture = new Gtk.GestureClick ();
            drag_gesture.released.connect (() => {
                if (user_dragging) {
                    user_dragging = false;
                    var val = (int64) slider.get_value ();
                    _position_ms = val;
                    seek_requested (val);
                }
            });
            slider.add_controller (drag_gesture);

            append (slider);

            /* Time labels row */
            var time_row = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 0);

            current_label = new Gtk.Label ("0:00");
            current_label.add_css_class ("text-timestamp");
            current_label.halign = Gtk.Align.START;
            current_label.hexpand = true;
            time_row.append (current_label);

            remaining_label = new Gtk.Label ("-0:00");
            remaining_label.add_css_class ("text-timestamp");
            remaining_label.halign = Gtk.Align.END;
            time_row.append (remaining_label);

            append (time_row);
        }

        ~SeekBar () {
            stop_interpolation ();
        }

        private void start_interpolation () {
            if (interpolation_id != 0) return;

            interpolation_id = Timeout.add (100, () => {
                if (!_is_playing || user_dragging) return Source.CONTINUE;

                _position_ms += 100;
                if (_duration_ms > 0 && _position_ms > _duration_ms) {
                    _position_ms = _duration_ms;
                }

                update_slider ();
                update_labels ();
                return Source.CONTINUE;
            });
        }

        private void stop_interpolation () {
            if (interpolation_id != 0) {
                Source.remove (interpolation_id);
                interpolation_id = 0;
            }
        }

        private void update_slider () {
            slider.set_value ((double) _position_ms);
        }

        private void update_labels () {
            current_label.label = format_time (_position_ms);

            var remaining = _duration_ms - _position_ms;
            if (remaining < 0) remaining = 0;
            remaining_label.label = "-%s".printf (format_time (remaining));
        }

        /**
         * Format milliseconds as "m:ss" or "h:mm:ss".
         */
        private string format_time (int64 ms) {
            var total_seconds = (int64) (ms / 1000);
            if (total_seconds < 0) total_seconds = 0;

            var hours = total_seconds / 3600;
            var minutes = (total_seconds % 3600) / 60;
            var seconds = total_seconds % 60;

            if (hours > 0) {
                return "%lld:%02lld:%02lld".printf (hours, minutes, seconds);
            }
            return "%lld:%02lld".printf (minutes, seconds);
        }
    }
}
