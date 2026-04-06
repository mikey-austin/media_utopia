/* transport_controls.vala — Row of playback buttons plus volume control. */

namespace Mu {

    public class TransportControls : Gtk.Box {

        private Gtk.Button shuffle_btn;
        private Gtk.Button prev_btn;
        private Gtk.Button play_pause_btn;
        private Gtk.Image play_pause_icon;
        private Gtk.Button next_btn;
        private Gtk.Button repeat_btn;
        private Gtk.Image repeat_icon;
        private Gtk.Button volume_btn;
        private Gtk.Image volume_icon;
        private Gtk.Scale volume_scale;
        private Gtk.Label volume_label;

        private bool _is_playing = false;
        public bool is_playing {
            get { return _is_playing; }
            set {
                _is_playing = value;
                update_play_pause_icon ();
            }
        }

        private bool _shuffle = false;
        public bool shuffle {
            get { return _shuffle; }
            set {
                _shuffle = value;
                update_shuffle_class ();
            }
        }

        private string _repeat_mode = "off";
        public string repeat_mode {
            get { return _repeat_mode; }
            set {
                _repeat_mode = value;
                update_repeat_class ();
            }
        }

        private double _volume = 0.7;
        public double volume {
            get { return _volume; }
            set {
                _volume = value;
                /* Update scale without triggering signal loop */
                if ((volume_scale.get_value () - _volume).abs () > 0.005) {
                    volume_scale.set_value (_volume);
                }
                update_volume_label ();
            }
        }

        private bool _muted = false;
        public bool muted {
            get { return _muted; }
            set {
                _muted = value;
                update_volume_icon ();
            }
        }

        public signal void play_pause_clicked ();
        public signal void next_clicked ();
        public signal void prev_clicked ();
        public signal void shuffle_toggled ();
        public signal void repeat_toggled ();
        public signal void volume_changed (double volume);
        public signal void mute_toggled ();

        public TransportControls () {
            Object (orientation: Gtk.Orientation.HORIZONTAL, spacing: 8);
            valign = Gtk.Align.CENTER;

            build_transport_buttons ();
            build_volume_controls ();

            /* Apply initial states */
            update_play_pause_icon ();
            update_shuffle_class ();
            update_repeat_class ();
            update_volume_icon ();
            update_volume_label ();
        }

        private void build_transport_buttons () {
            /* Shuffle */
            shuffle_btn = new Gtk.Button.from_icon_name ("media-playlist-shuffle-symbolic");
            shuffle_btn.add_css_class ("transport-button");
            shuffle_btn.add_css_class ("flat");
            shuffle_btn.tooltip_text = "Shuffle";
            shuffle_btn.clicked.connect (() => shuffle_toggled ());
            append (shuffle_btn);

            /* Previous */
            prev_btn = new Gtk.Button.from_icon_name ("media-skip-backward-symbolic");
            prev_btn.add_css_class ("transport-button");
            prev_btn.add_css_class ("flat");
            prev_btn.tooltip_text = "Previous";
            prev_btn.clicked.connect (() => prev_clicked ());
            append (prev_btn);

            /* Play/Pause — primary button */
            play_pause_icon = new Gtk.Image.from_icon_name ("media-playback-start-symbolic");
            play_pause_icon.pixel_size = 28;
            play_pause_btn = new Gtk.Button ();
            play_pause_btn.child = play_pause_icon;
            play_pause_btn.add_css_class ("transport-button-play");
            play_pause_btn.add_css_class ("circular");
            play_pause_btn.tooltip_text = "Play";
            play_pause_btn.clicked.connect (() => play_pause_clicked ());
            append (play_pause_btn);

            /* Next */
            next_btn = new Gtk.Button.from_icon_name ("media-skip-forward-symbolic");
            next_btn.add_css_class ("transport-button");
            next_btn.add_css_class ("flat");
            next_btn.tooltip_text = "Next";
            next_btn.clicked.connect (() => next_clicked ());
            append (next_btn);

            /* Repeat */
            repeat_icon = new Gtk.Image.from_icon_name ("media-playlist-repeat-symbolic");
            repeat_btn = new Gtk.Button ();
            repeat_btn.child = repeat_icon;
            repeat_btn.add_css_class ("transport-button");
            repeat_btn.add_css_class ("flat");
            repeat_btn.tooltip_text = "Repeat";
            repeat_btn.clicked.connect (() => repeat_toggled ());
            append (repeat_btn);
        }

        private void build_volume_controls () {
            /* Spacer */
            var spacer = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 0);
            spacer.hexpand = true;
            append (spacer);

            /* Volume speaker icon (clickable for mute toggle) */
            volume_icon = new Gtk.Image.from_icon_name ("audio-volume-high-symbolic");
            volume_icon.pixel_size = 18;
            volume_btn = new Gtk.Button ();
            volume_btn.child = volume_icon;
            volume_btn.add_css_class ("flat");
            volume_btn.add_css_class ("transport-button");
            volume_btn.tooltip_text = "Mute";
            volume_btn.clicked.connect (() => mute_toggled ());
            append (volume_btn);

            /* Volume slider */
            volume_scale = new Gtk.Scale.with_range (Gtk.Orientation.HORIZONTAL, 0.0, 1.0, 0.01);
            volume_scale.draw_value = false;
            volume_scale.set_value (_volume);
            volume_scale.width_request = 100;
            volume_scale.add_css_class ("volume-scale");
            volume_scale.value_changed.connect (() => {
                var new_vol = volume_scale.get_value ();
                if ((new_vol - _volume).abs () > 0.005) {
                    _volume = new_vol;
                    update_volume_label ();
                    volume_changed (new_vol);
                }
            });
            append (volume_scale);

            /* Volume percentage label */
            volume_label = new Gtk.Label ("70%");
            volume_label.add_css_class ("text-timestamp");
            volume_label.width_chars = 4;
            volume_label.xalign = 1.0f;
            append (volume_label);
        }

        private void update_play_pause_icon () {
            if (_is_playing) {
                play_pause_icon.icon_name = "media-playback-pause-symbolic";
                play_pause_btn.tooltip_text = "Pause";
            } else {
                play_pause_icon.icon_name = "media-playback-start-symbolic";
                play_pause_btn.tooltip_text = "Play";
            }
        }

        private void update_shuffle_class () {
            if (_shuffle) {
                shuffle_btn.add_css_class ("text-accent");
            } else {
                shuffle_btn.remove_css_class ("text-accent");
            }
        }

        private void update_repeat_class () {
            if (_repeat_mode != "off") {
                repeat_btn.add_css_class ("text-accent");
                if (_repeat_mode == "one") {
                    repeat_btn.tooltip_text = "Repeat: One";
                } else {
                    repeat_btn.tooltip_text = "Repeat: All";
                }
            } else {
                repeat_btn.remove_css_class ("text-accent");
                repeat_btn.tooltip_text = "Repeat";
            }
        }

        private void update_volume_icon () {
            if (_muted) {
                volume_icon.icon_name = "audio-volume-muted-symbolic";
                volume_btn.tooltip_text = "Unmute";
            } else {
                volume_icon.icon_name = "audio-volume-high-symbolic";
                volume_btn.tooltip_text = "Mute";
            }
        }

        private void update_volume_label () {
            var pct = (int) (_volume * 100);
            volume_label.label = "%d%%".printf (pct);
        }
    }
}
