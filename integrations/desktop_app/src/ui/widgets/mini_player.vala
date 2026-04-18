/* mini_player.vala — Mu.MiniPlayer : Gtk.Box
 * Persistent bottom bar with current track info and compact transport controls.
 * Shown on all views except Now Playing when a track is loaded.
 */

namespace Mu {

    public class MiniPlayer : Gtk.Box {

        /* Service dependencies */
        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_repo;
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;
        private ArtworkLoader artwork_loader;

        /* Layout widgets */
        private Gtk.Box art_box;
        private Gtk.Image art_icon;
        private Gtk.Picture art_picture;
        private Gtk.Label title_label;
        private Gtk.Label artist_label;
        private Gtk.Button prev_button;
        private Gtk.Button play_pause_button;
        private Gtk.Button next_button;

        /* Current playback state */
        private bool is_playing = false;
        private bool has_track = false;

        /* Signal handler IDs */
        private ulong state_changed_id = 0;
        private ulong active_changed_id = 0;

        /* Track last loaded artwork URL to avoid redundant loads */
        private string last_artwork_url = "";

        /* Signals */
        public signal void clicked ();           /* Navigate to Now Playing */
        public signal void play_pause_clicked ();
        public signal void next_clicked ();
        public signal void prev_clicked ();

        public MiniPlayer (RendererStateRepository state_repo,
                           ActiveRendererRepository active_repo,
                           CommandCorrelator correlator,
                           LeaseManager lease_mgr,
                           ArtworkLoader artwork_loader) {
            Object (orientation: Gtk.Orientation.HORIZONTAL, spacing: 0);

            this.state_repo = state_repo;
            this.active_repo = active_repo;
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
            this.artwork_loader = artwork_loader;

            build_ui ();
            connect_signals ();

            /* Apply initial state */
            refresh_from_state ();
        }

        public void update_state (RendererState? state) {
            if (state == null) {
                has_track = false;
                return;
            }
            apply_state (state);
        }

        /* ---- UI construction ---- */

        private void build_ui () {
            add_css_class ("mini-player");
            height_request = 56;

            /* Click gesture on the whole bar to navigate to Now Playing */
            var click = new Gtk.GestureClick ();
            click.pressed.connect ((n_press, x, y) => {
                /* Check if the click is over a button — if so, let the button handle it */
                var widget = pick (x, y, Gtk.PickFlags.DEFAULT);
                if (widget is Gtk.Button) return;

                /* Walk parents in case the icon inside the button was picked */
                var parent = widget;
                while (parent != null && parent != this) {
                    if (parent is Gtk.Button) return;
                    parent = parent.get_parent ();
                }

                clicked ();
            });
            add_controller (click);

            /* --- Artwork thumbnail (40x40) --- */
            art_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            art_box.width_request = 40;
            art_box.height_request = 40;
            art_box.halign = Gtk.Align.CENTER;
            art_box.valign = Gtk.Align.CENTER;
            art_box.overflow = Gtk.Overflow.HIDDEN;
            art_box.add_css_class ("queue-art");  /* reuse the rounded dark placeholder style */

            art_icon = new Gtk.Image.from_icon_name ("media-optical-symbolic");
            art_icon.pixel_size = 20;
            art_icon.add_css_class ("text-secondary");
            art_icon.halign = Gtk.Align.CENTER;
            art_icon.valign = Gtk.Align.CENTER;
            art_icon.hexpand = true;
            art_icon.vexpand = true;
            art_box.append (art_icon);

            art_picture = new Gtk.Picture ();
            art_picture.content_fit = Gtk.ContentFit.COVER;
            art_picture.width_request = 40;
            art_picture.height_request = 40;
            art_picture.visible = false;
            art_box.append (art_picture);

            append (art_box);

            /* --- Title + Artist vertical box --- */
            var info_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 2);
            info_box.hexpand = true;
            info_box.valign = Gtk.Align.CENTER;
            info_box.margin_start = 12;
            info_box.margin_end = 12;

            title_label = new Gtk.Label ("");
            title_label.add_css_class ("mini-player-title");
            title_label.halign = Gtk.Align.START;
            title_label.ellipsize = Pango.EllipsizeMode.END;
            title_label.max_width_chars = 40;
            title_label.single_line_mode = true;
            info_box.append (title_label);

            artist_label = new Gtk.Label ("");
            artist_label.add_css_class ("mini-player-artist");
            artist_label.halign = Gtk.Align.START;
            artist_label.ellipsize = Pango.EllipsizeMode.END;
            artist_label.max_width_chars = 40;
            artist_label.single_line_mode = true;
            info_box.append (artist_label);

            append (info_box);

            /* --- Transport controls --- */
            var transport_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 4);
            transport_box.valign = Gtk.Align.CENTER;
            transport_box.margin_end = 4;

            /* Previous button */
            prev_button = new Gtk.Button.from_icon_name ("media-skip-backward-symbolic");
            prev_button.add_css_class ("flat");
            prev_button.add_css_class ("circular");
            prev_button.tooltip_text = "Previous";
            prev_button.clicked.connect (on_prev);
            transport_box.append (prev_button);

            /* Play/Pause button */
            play_pause_button = new Gtk.Button.from_icon_name ("media-playback-start-symbolic");
            play_pause_button.add_css_class ("circular");
            play_pause_button.add_css_class ("suggested-action");
            play_pause_button.width_request = 32;
            play_pause_button.height_request = 32;
            play_pause_button.tooltip_text = "Play";
            play_pause_button.clicked.connect (on_play_pause);
            transport_box.append (play_pause_button);

            /* Next button */
            next_button = new Gtk.Button.from_icon_name ("media-skip-forward-symbolic");
            next_button.add_css_class ("flat");
            next_button.add_css_class ("circular");
            next_button.tooltip_text = "Next";
            next_button.clicked.connect (on_next);
            transport_box.append (next_button);

            append (transport_box);
        }

        /* ---- Signal wiring ---- */

        private void connect_signals () {
            state_changed_id = state_repo.state_changed.connect ((node_id, state) => {
                if (node_id == active_repo.active_renderer_id) {
                    apply_state (state);
                }
            });

            active_changed_id = active_repo.active_renderer_changed.connect ((node_id) => {
                refresh_from_state ();
            });
        }

        /* ---- State application ---- */

        private void refresh_from_state () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) {
                has_track = false;
                return;
            }

            var state = state_repo.get_state (renderer_id);
            if (state == null) {
                has_track = false;
                return;
            }

            apply_state (state);
        }

        private void apply_state (RendererState state) {
            /* Update display from current item */
            if (state.current != null && state.current.display != null) {
                var display = state.current.display;

                var track_title = display.title;
                var artist = display.artist_display ();

                if (track_title.length > 0) {
                    title_label.label = track_title;
                    artist_label.label = artist;
                    has_track = true;
                } else {
                    has_track = false;
                }

                /* Load artwork thumbnail */
                var art_url = display.artwork_url;
                if (art_url.length > 0 && art_url != last_artwork_url) {
                    last_artwork_url = art_url;
                    var requested_url = art_url;
                    artwork_loader.load_async (art_url, (texture) => {
                        if (requested_url != last_artwork_url) {
                            return;
                        }
                        if (texture != null) {
                            art_picture.paintable = texture;
                            art_picture.visible = true;
                            art_icon.visible = false;
                        } else {
                            art_picture.visible = false;
                            art_icon.visible = true;
                        }
                    });
                } else if (art_url.length == 0) {
                    last_artwork_url = "";
                    art_picture.visible = false;
                    art_icon.visible = true;
                }
            } else {
                has_track = false;
                last_artwork_url = "";
                art_picture.visible = false;
                art_icon.visible = true;
            }

            /* Playback state */
            if (state.playback != null) {
                is_playing = state.playback.status == "playing";
                update_play_pause_icon ();
            }
        }

        private void update_play_pause_icon () {
            if (is_playing) {
                play_pause_button.icon_name = "media-playback-pause-symbolic";
                play_pause_button.tooltip_text = "Pause";
            } else {
                play_pause_button.icon_name = "media-playback-start-symbolic";
                play_pause_button.tooltip_text = "Play";
            }
        }

        /* ---- Command dispatch ---- */

        private void send_command (string cmd_type, Json.Object body) {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            lease_mgr.ensure_lease.begin (renderer_id, (obj, res) => {
                var lease = lease_mgr.ensure_lease.end (res);
                if (lease != null) {
                    correlator.send_fire_and_forget (renderer_id, cmd_type, body, lease);
                }
            });
        }

        private void on_play_pause () {
            if (is_playing) {
                send_command ("playback.pause", new Json.Object ());
            } else {
                send_command ("playback.play", PlaybackBodies.play ());
            }
        }

        private void on_next () {
            send_command ("playback.next", new Json.Object ());
        }

        private void on_prev () {
            send_command ("playback.prev", new Json.Object ());
        }

        /**
         * Whether the mini player has a track to display.
         * Used by the window to determine visibility.
         */
        public bool get_has_track () {
            return has_track;
        }
    }
}
