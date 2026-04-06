/* now_playing_view.vala — Mu.NowPlayingView : Adw.Bin
 * Full now-playing view with artwork, metadata, seek bar, transport, volume.
 */

namespace Mu {

    public class NowPlayingView : Adw.Bin {

        /* Service dependencies */
        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_repo;
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;
        private LocalRenderer? local_renderer;

        /* Layout widgets */
        private Gtk.Picture artwork;
        private HiResBadge hires_badge;
        private Gtk.Label title_label;
        private Gtk.Label artist_label;
        private Gtk.Label album_label;
        private AudioVisualizer visualizer;
        private SeekBar seek_bar;
        private TransportControls transport;

        /* Placeholder shown when nothing is playing */
        private Gtk.Box placeholder_box;
        private Gtk.Box metadata_box;

        /* Signal handler IDs */
        private ulong state_changed_id = 0;
        private ulong active_changed_id = 0;
        private ulong spectrum_handler_id = 0;

        /* Volume debounce */
        private uint volume_debounce_id = 0;

        public NowPlayingView (RendererStateRepository state_repo,
                               ActiveRendererRepository active_repo,
                               CommandCorrelator correlator,
                               LeaseManager lease_mgr,
                               LocalRenderer? local_renderer = null) {
            this.state_repo = state_repo;
            this.active_repo = active_repo;
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
            this.local_renderer = local_renderer;

            build_ui ();
            connect_signals ();

            /* Apply initial state if a renderer is already active */
            refresh_from_state ();
        }

        private void build_ui () {
            add_css_class ("now-playing-view");

            /* Main horizontal layout: artwork | info */
            var hbox = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 32);
            hbox.hexpand = true;
            hbox.vexpand = true;
            hbox.valign = Gtk.Align.CENTER;
            hbox.halign = Gtk.Align.CENTER;
            hbox.margin_start = 40;
            hbox.margin_end = 40;
            hbox.margin_top = 24;
            hbox.margin_bottom = 24;

            /* --- Left side: album artwork area --- */
            var art_frame = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            art_frame.width_request = 340;
            art_frame.halign = Gtk.Align.CENTER;
            art_frame.valign = Gtk.Align.CENTER;

            /* Overlay for artwork + hires badge */
            var art_overlay = new Gtk.Overlay ();

            /* Artwork container with rounded corners and dark placeholder bg */
            var art_bg = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            art_bg.add_css_class ("album-art");
            art_bg.width_request = 340;
            art_bg.height_request = 340;
            art_bg.halign = Gtk.Align.CENTER;
            art_bg.valign = Gtk.Align.CENTER;
            art_bg.overflow = Gtk.Overflow.HIDDEN;

            /* Album art placeholder icon */
            var art_placeholder = new Gtk.Image.from_icon_name ("media-optical-symbolic");
            art_placeholder.pixel_size = 80;
            art_placeholder.add_css_class ("text-secondary");
            art_placeholder.hexpand = true;
            art_placeholder.vexpand = true;
            art_placeholder.halign = Gtk.Align.CENTER;
            art_placeholder.valign = Gtk.Align.CENTER;

            artwork = new Gtk.Picture ();
            artwork.content_fit = Gtk.ContentFit.COVER;
            artwork.width_request = 340;
            artwork.height_request = 340;
            artwork.visible = false;  /* Hidden until artwork is loaded (Task 18) */

            art_bg.append (art_placeholder);
            art_bg.append (artwork);
            art_overlay.child = art_bg;

            /* HiRes badge overlaid in top-right corner */
            hires_badge = new HiResBadge ();
            hires_badge.halign = Gtk.Align.END;
            hires_badge.valign = Gtk.Align.START;
            hires_badge.margin_top = 8;
            hires_badge.margin_end = 8;
            art_overlay.add_overlay (hires_badge);

            art_frame.append (art_overlay);
            hbox.append (art_frame);

            /* --- Right side: metadata + controls --- */
            var right_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            right_box.hexpand = true;
            right_box.valign = Gtk.Align.CENTER;
            right_box.width_request = 300;

            /* Placeholder for when nothing is playing */
            placeholder_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 12);
            placeholder_box.halign = Gtk.Align.CENTER;
            placeholder_box.valign = Gtk.Align.CENTER;
            placeholder_box.vexpand = true;

            var placeholder_icon = new Gtk.Image.from_resource (
                "/com/mediautopia/desktop/icons/mu-motif.svg");
            placeholder_icon.pixel_size = 64;
            placeholder_icon.margin_bottom = 8;
            placeholder_box.append (placeholder_icon);

            var placeholder_title = new Gtk.Label ("Nothing Playing");
            placeholder_title.add_css_class ("heading-medium");
            placeholder_box.append (placeholder_title);

            var placeholder_sub = new Gtk.Label ("Select a track to begin");
            placeholder_sub.add_css_class ("text-secondary");
            placeholder_box.append (placeholder_sub);

            right_box.append (placeholder_box);

            /* Metadata + controls (hidden until something plays) */
            metadata_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            metadata_box.visible = false;
            metadata_box.vexpand = true;
            metadata_box.valign = Gtk.Align.CENTER;

            /* Track title */
            title_label = new Gtk.Label ("");
            title_label.add_css_class ("track-title");
            title_label.halign = Gtk.Align.START;
            title_label.ellipsize = Pango.EllipsizeMode.END;
            title_label.max_width_chars = 40;
            metadata_box.append (title_label);

            /* Artist */
            artist_label = new Gtk.Label ("");
            artist_label.add_css_class ("track-artist");
            artist_label.halign = Gtk.Align.START;
            artist_label.ellipsize = Pango.EllipsizeMode.END;
            artist_label.max_width_chars = 40;
            artist_label.margin_top = 4;
            metadata_box.append (artist_label);

            /* Album */
            album_label = new Gtk.Label ("");
            album_label.add_css_class ("track-album");
            album_label.halign = Gtk.Align.START;
            album_label.ellipsize = Pango.EllipsizeMode.END;
            album_label.max_width_chars = 40;
            album_label.margin_top = 2;
            metadata_box.append (album_label);

            /* Spacer */
            var spacer = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            spacer.height_request = 24;
            metadata_box.append (spacer);

            /* Audio visualizer (28-bar FFT display) */
            visualizer = new AudioVisualizer ();
            metadata_box.append (visualizer);

            /* Seek bar */
            seek_bar = new SeekBar ();
            seek_bar.margin_top = 8;
            seek_bar.seek_requested.connect (on_seek_requested);
            metadata_box.append (seek_bar);

            /* Transport controls */
            transport = new TransportControls ();
            transport.margin_top = 16;
            transport.play_pause_clicked.connect (on_play_pause);
            transport.next_clicked.connect (on_next);
            transport.prev_clicked.connect (on_prev);
            transport.shuffle_toggled.connect (on_shuffle_toggled);
            transport.repeat_toggled.connect (on_repeat_toggled);
            transport.volume_changed.connect (on_volume_changed);
            transport.mute_toggled.connect (on_mute_toggled);
            metadata_box.append (transport);

            right_box.append (metadata_box);
            hbox.append (right_box);

            this.child = hbox;
        }

        private void connect_signals () {
            state_changed_id = state_repo.state_changed.connect ((node_id, state) => {
                if (node_id == active_repo.active_renderer_id) {
                    apply_state (state);
                }
            });

            active_changed_id = active_repo.active_renderer_changed.connect ((node_id) => {
                refresh_from_state ();
            });

            /* Wire spectrum data from local renderer to visualizer */
            if (local_renderer != null) {
                spectrum_handler_id = local_renderer.spectrum_data.connect ((mags) => {
                    /* Only feed visualizer when the active renderer IS the local renderer */
                    if (active_repo.active_renderer_id == local_renderer.node_id) {
                        visualizer.update_magnitudes (mags);
                    }
                });
            }
        }

        /* ---- State application ---- */

        private void refresh_from_state () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) {
                show_placeholder ();
                return;
            }

            var state = state_repo.get_state (renderer_id);
            if (state == null) {
                show_placeholder ();
                return;
            }

            apply_state (state);
        }

        private void show_placeholder () {
            placeholder_box.visible = true;
            metadata_box.visible = false;
            visualizer.clear ();
        }

        private void apply_state (RendererState state) {
            /* Update metadata from current item */
            if (state.current != null && state.current.metadata != null) {
                var meta = state.current.metadata;

                var track_title = meta.has_member ("title")
                    ? meta.get_string_member ("title") : "";
                var artist = meta.has_member ("artist")
                    ? meta.get_string_member ("artist") : "";
                var album = meta.has_member ("album")
                    ? meta.get_string_member ("album") : "";

                if (track_title.length > 0) {
                    title_label.label = track_title;
                    artist_label.label = artist;
                    album_label.label = album.up ();

                    placeholder_box.visible = false;
                    metadata_box.visible = true;
                } else {
                    show_placeholder ();
                    return;
                }

                /* HiRes badge */
                hires_badge.update_from_metadata (meta);
            } else {
                show_placeholder ();
                return;
            }

            /* Playback state */
            if (state.playback != null) {
                var pb = state.playback;
                var playing = pb.status == "playing";

                seek_bar.duration_ms = pb.duration_ms;
                seek_bar.position_ms = pb.position_ms;
                seek_bar.is_playing = playing;

                transport.is_playing = playing;
                transport.volume = pb.volume;
                transport.muted = pb.mute;

                /* Clear visualizer when not playing or when on a remote renderer */
                bool is_local = local_renderer != null &&
                    active_repo.active_renderer_id == local_renderer.node_id;
                if (!playing || !is_local) {
                    visualizer.clear ();
                }
            }

            /* Queue state */
            if (state.queue != null) {
                transport.shuffle = state.queue.shuffle;
                transport.repeat_mode = state.queue.repeat_mode;
            }
        }

        /* ---- Command dispatch ---- */

        /**
         * Helper: acquire lease and fire a command for the active renderer.
         */
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
            if (transport.is_playing) {
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

        private void on_seek_requested (int64 position_ms) {
            send_command ("playback.seek", PlaybackBodies.seek (position_ms));
        }

        private void on_volume_changed (double vol) {
            /* Debounce volume commands (50ms) */
            if (volume_debounce_id != 0) {
                Source.remove (volume_debounce_id);
            }

            volume_debounce_id = Timeout.add (50, () => {
                volume_debounce_id = 0;
                send_command ("playback.setVolume", PlaybackBodies.set_volume (vol));
                return Source.REMOVE;
            });
        }

        private void on_mute_toggled () {
            var new_mute = !transport.muted;
            send_command ("playback.setMute", PlaybackBodies.set_mute (new_mute));
        }

        private void on_shuffle_toggled () {
            var new_shuffle = !transport.shuffle;
            send_command ("queue.setShuffle", QueueBodies.set_shuffle (new_shuffle));
        }

        private void on_repeat_toggled () {
            /* Cycle: off -> all -> one -> off */
            string new_mode;
            switch (transport.repeat_mode) {
                case "off":
                    new_mode = "all";
                    break;
                case "all":
                    new_mode = "one";
                    break;
                default:
                    new_mode = "off";
                    break;
            }

            var repeat_on = (new_mode != "off");
            send_command ("queue.setRepeat", QueueBodies.set_repeat (repeat_on, new_mode));
        }
    }
}
