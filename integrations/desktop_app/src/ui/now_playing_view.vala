/* now_playing_view.vala — Mu.NowPlayingView : Adw.Bin
 * Full now-playing view with artwork, metadata, seek bar, transport, volume.
 */

namespace Mu {

    public class NowPlayingView : Adw.Bin {

        /* Service dependencies */
        private RendererStateRepository state_repo;
        private ZoneStateRepository zone_state_repo;
        private NodeRepository node_repo;
        private ActiveRendererRepository active_repo;
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;
        private LocalRenderer? local_renderer;
        private ArtworkLoader artwork_loader;

        /* Layout widgets */
        private Gtk.Picture artwork;
        private Gtk.Image art_placeholder;
        private HiResBadge hires_badge;
        private Gtk.Label title_label;
        private Gtk.Label artist_label;
        private Gtk.Label album_label;
        private AudioVisualizer visualizer;
        private SeekBar seek_bar;
        private TransportControls transport;
        private Gtk.Label route_renderer_label;
        private Gtk.Label route_session_label;
        private Gtk.Label route_hint_label;
        private Gtk.Label route_zone_count_label;
        private Gtk.Box route_zone_list;

        /* Placeholder shown when nothing is playing */
        private Gtk.Box placeholder_box;
        private Gtk.Box metadata_box;

        /* Foreign-lease banner */
        private Gtk.Box lease_banner;
        private Gtk.Label lease_banner_label;
        private bool lease_blocked = false;

        /* Signal handler IDs */
        private ulong state_changed_id = 0;
        private ulong active_changed_id = 0;
        private ulong spectrum_handler_id = 0;
        private ulong node_added_id = 0;
        private ulong node_removed_id = 0;
        private ulong node_updated_id = 0;
        private ulong zone_state_changed_id = 0;

        /* Volume debounce */
        private uint volume_debounce_id = 0;
        private HashTable<string, uint> zone_volume_timers;

        /* Track last loaded artwork URL to avoid redundant loads */
        private string last_artwork_url = "";

        public NowPlayingView (RendererStateRepository state_repo,
                               ZoneStateRepository zone_state_repo,
                               NodeRepository node_repo,
                               ActiveRendererRepository active_repo,
                               CommandCorrelator correlator,
                               LeaseManager lease_mgr,
                               ArtworkLoader artwork_loader,
                               LocalRenderer? local_renderer = null) {
            this.state_repo = state_repo;
            this.zone_state_repo = zone_state_repo;
            this.node_repo = node_repo;
            this.active_repo = active_repo;
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
            this.artwork_loader = artwork_loader;
            this.local_renderer = local_renderer;
            this.zone_volume_timers = new HashTable<string, uint> (str_hash, str_equal);

            build_ui ();
            connect_signals ();

            /* Apply initial state if a renderer is already active */
            refresh_from_state ();
        }

        private void build_ui () {
            add_css_class ("now-playing-view");

            /* Main horizontal layout: artwork | info | routing */
            var hbox = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 32);
            hbox.hexpand = true;
            hbox.vexpand = true;
            hbox.valign = Gtk.Align.FILL;
            hbox.halign = Gtk.Align.FILL;
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
            art_placeholder = new Gtk.Image.from_icon_name ("media-optical-symbolic");
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
            right_box.width_request = 380;

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

            /* Foreign-lease banner: shown when another controller owns the
             * session; offers takeControl like the Android Now Playing. */
            lease_banner = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            lease_banner.add_css_class ("lease-banner");
            lease_banner.margin_top = 12;
            lease_banner.visible = false;

            lease_banner_label = new Gtk.Label ("");
            lease_banner_label.halign = Gtk.Align.START;
            lease_banner_label.hexpand = true;
            lease_banner_label.wrap = true;
            lease_banner.append (lease_banner_label);

            var take_control_btn = new Gtk.Button.with_label ("Take Control");
            take_control_btn.add_css_class ("gradient-cta");
            take_control_btn.valign = Gtk.Align.CENTER;
            take_control_btn.clicked.connect (on_take_control);
            lease_banner.append (take_control_btn);

            metadata_box.append (lease_banner);

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

            /* --- Desktop routing panel --- */
            var route_panel = new Gtk.Box (Gtk.Orientation.VERTICAL, 12);
            route_panel.add_css_class ("now-playing-side-panel");
            route_panel.width_request = 320;
            route_panel.valign = Gtk.Align.FILL;

            var route_title = new Gtk.Label ("Playback Routing");
            route_title.add_css_class ("heading-medium");
            route_title.halign = Gtk.Align.START;
            route_panel.append (route_title);

            var route_card = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
            route_card.add_css_class ("mu-card");
            route_card.add_css_class ("now-playing-side-card");

            route_renderer_label = new Gtk.Label ("No renderer selected");
            route_renderer_label.add_css_class ("renderer-name");
            route_renderer_label.halign = Gtk.Align.START;
            route_renderer_label.ellipsize = Pango.EllipsizeMode.END;
            route_card.append (route_renderer_label);

            route_session_label = new Gtk.Label ("");
            route_session_label.add_css_class ("renderer-status");
            route_session_label.halign = Gtk.Align.START;
            route_session_label.wrap = true;
            route_card.append (route_session_label);

            route_hint_label = new Gtk.Label ("");
            route_hint_label.add_css_class ("meta-label");
            route_hint_label.halign = Gtk.Align.START;
            route_hint_label.wrap = true;
            route_card.append (route_hint_label);

            route_panel.append (route_card);

            var zones_title_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);

            var zones_title = new Gtk.Label ("Zones");
            zones_title.add_css_class ("heading-small");
            zones_title.halign = Gtk.Align.START;
            zones_title.hexpand = true;
            zones_title_box.append (zones_title);

            route_zone_count_label = new Gtk.Label ("");
            route_zone_count_label.add_css_class ("meta-label");
            route_zone_count_label.halign = Gtk.Align.END;
            zones_title_box.append (route_zone_count_label);

            route_panel.append (zones_title_box);

            var zones_scroll = new Gtk.ScrolledWindow ();
            zones_scroll.hscrollbar_policy = Gtk.PolicyType.NEVER;
            zones_scroll.vscrollbar_policy = Gtk.PolicyType.AUTOMATIC;
            zones_scroll.hexpand = true;
            zones_scroll.vexpand = true;
            zones_scroll.min_content_height = 260;

            route_zone_list = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
            route_zone_list.add_css_class ("now-playing-zone-list");
            zones_scroll.child = route_zone_list;
            route_panel.append (zones_scroll);

            hbox.append (route_panel);

            this.child = hbox;
        }

        private void connect_signals () {
            state_changed_id = state_repo.state_changed.connect ((node_id, state) => {
                if (node_id == active_repo.active_renderer_id) {
                    apply_state (state);
                    refresh_route_summary (state);
                }
            });

            active_changed_id = active_repo.active_renderer_changed.connect ((node_id) => {
                refresh_from_state ();
                rebuild_route_panel ();
            });

            node_added_id = node_repo.node_added.connect ((presence) => {
                if (presence.kind == "zone" || presence.node_id == active_repo.active_renderer_id) {
                    rebuild_route_panel ();
                }
            });

            node_removed_id = node_repo.node_removed.connect ((node_id) => {
                if (node_id == active_repo.active_renderer_id) {
                    refresh_route_summary (null);
                }
                rebuild_route_panel ();
            });

            node_updated_id = node_repo.node_updated.connect ((presence) => {
                if (presence.kind == "zone" || presence.node_id == active_repo.active_renderer_id ||
                    presence.kind == "zone_controller") {
                    rebuild_route_panel ();
                }
            });

            zone_state_changed_id = zone_state_repo.state_changed.connect ((node_id, state) => {
                var presence = node_repo.get_node (node_id);
                if (presence != null && presence.kind == "zone") {
                    rebuild_route_zone_list ();
                }
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

            refresh_route_summary (null);
            rebuild_route_zone_list ();
        }

        public override void dispose () {
            if (state_changed_id != 0) {
                state_repo.disconnect (state_changed_id);
                state_changed_id = 0;
            }
            if (active_changed_id != 0) {
                active_repo.disconnect (active_changed_id);
                active_changed_id = 0;
            }
            if (node_added_id != 0) {
                node_repo.disconnect (node_added_id);
                node_added_id = 0;
            }
            if (node_removed_id != 0) {
                node_repo.disconnect (node_removed_id);
                node_removed_id = 0;
            }
            if (node_updated_id != 0) {
                node_repo.disconnect (node_updated_id);
                node_updated_id = 0;
            }
            if (zone_state_changed_id != 0) {
                zone_state_repo.disconnect (zone_state_changed_id);
                zone_state_changed_id = 0;
            }
            if (spectrum_handler_id != 0 && local_renderer != null) {
                local_renderer.disconnect (spectrum_handler_id);
                spectrum_handler_id = 0;
            }
            if (volume_debounce_id != 0) {
                Source.remove (volume_debounce_id);
                volume_debounce_id = 0;
            }
            if (zone_volume_timers != null) {
                zone_volume_timers.foreach ((zone_id, timer_id) => {
                    Source.remove (timer_id);
                });
                zone_volume_timers.remove_all ();
            }
            base.dispose ();
        }

        /* ---- State application ---- */

        private void refresh_from_state () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) {
                show_placeholder ();
                refresh_route_summary (null);
                return;
            }

            var state = state_repo.get_state (renderer_id);
            if (state == null) {
                show_placeholder ();
                refresh_route_summary (null);
                return;
            }

            apply_state (state);
            refresh_route_summary (state);
        }

        private void show_placeholder () {
            placeholder_box.visible = true;
            metadata_box.visible = false;
            visualizer.clear ();
            last_artwork_url = "";
            artwork.visible = false;
            art_placeholder.visible = true;
            update_lease_blocked (null);
            refresh_route_summary (null);
        }

        private void apply_state (RendererState state) {
            /* Update display metadata from current item */
            if (state.current != null && state.current.display != null) {
                var display = state.current.display;

                var track_title = display.title;
                var artist = display.artist_display ();
                var album = display.album;

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

                /* HiRes badge from optional format fields in the display block */
                hires_badge.update_from_display (display);

                /* Load artwork */
                var art_url = display.artwork_url;
                if (art_url.length > 0 && art_url != last_artwork_url) {
                    last_artwork_url = art_url;
                    var requested_url = art_url;
                    artwork_loader.load_async (art_url, (texture) => {
                        if (requested_url != last_artwork_url) {
                            return;
                        }
                        if (texture != null) {
                            artwork.paintable = texture;
                            artwork.visible = true;
                            art_placeholder.visible = false;
                        } else {
                            artwork.visible = false;
                            art_placeholder.visible = true;
                        }
                    });
                } else if (art_url.length == 0) {
                    last_artwork_url = "";
                    artwork.visible = false;
                    art_placeholder.visible = true;
                }
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

            update_lease_blocked (state);
        }

        /* ---- Foreign-lease handling ---- */

        private void update_lease_blocked (RendererState? state) {
            var own_identity = correlator.get_identity ();
            var owner = (state != null && state.session != null)
                ? state.session.owner : "";

            var blocked = owner.length > 0 && owner != own_identity;

            if (blocked) {
                lease_banner_label.label = "Controlled by %s".printf (owner);
            }

            if (blocked == lease_blocked) return;
            lease_blocked = blocked;

            transport.sensitive = !blocked;
            seek_bar.sensitive = !blocked;
            lease_banner.visible = blocked;
        }

        private void on_take_control () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            lease_mgr.take_control.begin (renderer_id, (obj, res) => {
                var lease = lease_mgr.take_control.end (res);
                if (lease == null) {
                    Toaster.show ("Couldn't take control of this renderer");
                    return;
                }
                Toaster.show ("You now control this renderer");
                /* State publish will flip the banner off; update eagerly too */
                update_lease_blocked (state_repo.get_state (renderer_id));
            });
        }

        /* ---- Routing panel ---- */

        private void rebuild_route_panel () {
            refresh_route_summary (state_repo.get_state (active_repo.active_renderer_id));
            rebuild_route_zone_list ();
        }

        private void refresh_route_summary (RendererState? state) {
            if (route_renderer_label == null) return;

            var renderer_name = get_active_renderer_name ();
            route_renderer_label.label = renderer_name;

            if (state != null && state.session != null && state.session.owner.length > 0) {
                route_session_label.label = "Session owner: %s".printf (state.session.owner);
                route_session_label.visible = true;
            } else {
                route_session_label.label = "Ready to take control";
                route_session_label.visible = true;
            }

            var renderer_source_id = get_active_renderer_source_id ();
            if (renderer_source_id.length > 0) {
                route_hint_label.label = "Zone assignment enabled for source %s"
                    .printf (humanize_source_id (renderer_source_id).up ());
            } else {
                route_hint_label.label = "Zone assignment unavailable for this renderer";
            }
        }

        private void rebuild_route_zone_list () {
            if (route_zone_list == null) return;

            Gtk.Widget? child = route_zone_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                route_zone_list.remove (child);
                child = next;
            }

            var zones = node_repo.get_zones ();
            uint zone_count = 0;
            for (uint i = 0; i < zones.length; i++) {
                var zone = zones[i];
                if (zone.kind != "zone") continue;
                zone_count++;
                route_zone_list.append (build_route_zone_row (zone));
            }

            route_zone_count_label.label = zone_count == 0
                ? "No zones" : "%u discovered".printf (zone_count);

            if (zone_count == 0) {
                var empty = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
                empty.add_css_class ("mu-card");
                empty.add_css_class ("now-playing-side-card");

                var title = new Gtk.Label ("No zones discovered");
                title.add_css_class ("heading-small");
                title.halign = Gtk.Align.START;
                empty.append (title);

                var subtitle = new Gtk.Label (
                    "Zone controls appear here when a zone controller is online.");
                subtitle.add_css_class ("meta-label");
                subtitle.halign = Gtk.Align.START;
                subtitle.wrap = true;
                empty.append (subtitle);

                route_zone_list.append (empty);
            }
        }

        private Gtk.Box build_route_zone_row (Presence zone) {
            var connected = get_zone_connected (zone.node_id);
            var renderer_source_id = get_active_renderer_source_id ();
            var assignment_supported = renderer_source_id.length > 0;
            var zone_source_id = get_zone_source_id (zone.node_id);
            var assigned = assignment_supported && zone_source_id == renderer_source_id;

            var card = new Gtk.Box (Gtk.Orientation.VERTICAL, 8);
            card.add_css_class ("mu-card");
            card.add_css_class ("now-playing-zone-row");
            if (assigned) {
                card.add_css_class ("active");
            }
            if (!connected) {
                card.add_css_class ("zone-card-offline");
            }

            var header = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);

            if (assignment_supported) {
                var toggle = new Gtk.CheckButton ();
                toggle.active = assigned;
                toggle.sensitive = connected;
                toggle.valign = Gtk.Align.START;
                toggle.toggled.connect (() => {
                    send_zone_source_command (zone.node_id, toggle.active
                        ? renderer_source_id : "");
                });
                header.append (toggle);
            }

            var text_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 2);
            text_box.hexpand = true;

            var name_label = new Gtk.Label (zone.name);
            name_label.add_css_class ("heading-small");
            name_label.halign = Gtk.Align.START;
            name_label.ellipsize = Pango.EllipsizeMode.END;
            text_box.append (name_label);

            var source_label = new Gtk.Label (get_zone_current_source_name (zone));
            source_label.add_css_class ("meta-label");
            source_label.halign = Gtk.Align.START;
            source_label.ellipsize = Pango.EllipsizeMode.END;
            text_box.append (source_label);

            header.append (text_box);

            var status_label = new Gtk.Label (connected
                ? "%d%%".printf ((int) (get_zone_volume (zone.node_id) * 100.0))
                : "OFFLINE");
            status_label.add_css_class ("meta-label");
            status_label.halign = Gtk.Align.END;
            header.append (status_label);

            card.append (header);

            var controls = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            controls.sensitive = connected;

            var mute_btn = new Gtk.Button ();
            mute_btn.add_css_class ("flat");
            mute_btn.icon_name = get_zone_muted (zone.node_id)
                ? "audio-volume-muted-symbolic" : "audio-volume-high-symbolic";
            mute_btn.tooltip_text = get_zone_muted (zone.node_id) ? "Unmute" : "Mute";
            mute_btn.clicked.connect (() => {
                send_zone_mute_command (zone.node_id, !get_zone_muted (zone.node_id));
            });
            controls.append (mute_btn);

            var scale = new Gtk.Scale.with_range (
                Gtk.Orientation.HORIZONTAL, 0.0, 100.0, 1.0);
            scale.hexpand = true;
            scale.draw_value = false;
            scale.add_css_class ("volume-scale");
            scale.set_value (get_zone_volume (zone.node_id) * 100.0);
            scale.value_changed.connect (() => {
                debounce_zone_volume (zone.node_id, scale.get_value () / 100.0);
                status_label.label = "%d%%".printf ((int) scale.get_value ());
            });
            controls.append (scale);

            card.append (controls);

            return card;
        }

        private void debounce_zone_volume (string zone_id, double volume) {
            var existing = zone_volume_timers.lookup (zone_id);
            if (existing != 0) {
                Source.remove (existing);
            }

            var timer_id = Timeout.add (150, () => {
                zone_volume_timers.remove (zone_id);
                send_zone_volume_command (zone_id, volume);
                return Source.REMOVE;
            });
            zone_volume_timers.set (zone_id, timer_id);
        }

        private void send_zone_volume_command (string zone_id, double volume) {
            var body = new Json.Object ();
            body.set_string_member ("zoneId", zone_id);
            body.set_double_member ("volume", volume);
            send_zone_command ("zone.setVolume", zone_id, body);
        }

        private void send_zone_mute_command (string zone_id, bool mute) {
            var body = new Json.Object ();
            body.set_string_member ("zoneId", zone_id);
            body.set_boolean_member ("mute", mute);
            send_zone_command ("zone.setMute", zone_id, body);
        }

        private void send_zone_source_command (string zone_id, string source_id) {
            var body = new Json.Object ();
            body.set_string_member ("zoneId", zone_id);
            body.set_string_member ("sourceId", source_id);
            send_zone_command ("zone.selectSource", zone_id, body);
        }

        private void send_zone_command (string cmd_type, string zone_id, Json.Object body) {
            var controller_id = get_zone_controller_id (zone_id);
            if (controller_id == null || controller_id.length == 0) {
                warning ("NowPlayingView: no zone controller available for %s", zone_id);
                return;
            }
            correlator.send_fire_and_forget (controller_id, cmd_type, body);
        }

        private string get_active_renderer_name () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) {
                return "No renderer selected";
            }

            if (local_renderer != null && renderer_id == local_renderer.node_id) {
                return local_renderer.name;
            }

            var presence = node_repo.get_node (renderer_id);
            if (presence != null && presence.name.length > 0) {
                return presence.name;
            }

            return renderer_id;
        }

        private string get_active_renderer_source_id () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return "";

            var presence = node_repo.get_node (renderer_id);
            if (presence != null && presence.source != null) {
                return presence.source ?? "";
            }

            return "";
        }

        private bool get_zone_connected (string zone_id) {
            var state = zone_state_repo.get_state (zone_id);
            return state != null ? state.connected : true;
        }

        private double get_zone_volume (string zone_id) {
            var state = zone_state_repo.get_state (zone_id);
            return state != null ? state.volume : 0.0;
        }

        private bool get_zone_muted (string zone_id) {
            var state = zone_state_repo.get_state (zone_id);
            return state != null ? state.mute : false;
        }

        private string get_zone_source_id (string zone_id) {
            var state = zone_state_repo.get_state (zone_id);
            return state != null ? state.source_id : "";
        }

        private string get_zone_current_source_name (Presence zone) {
            var source_id = get_zone_source_id (zone.node_id);
            if (source_id.length == 0) return "No source selected";

            var sources = get_zone_sources (zone);
            if (sources != null) {
                for (uint i = 0; i < sources.length; i++) {
                    if (sources[i].id == source_id) {
                        return sources[i].name;
                    }
                }
            }

            return humanize_source_id (source_id);
        }

        private GenericArray<PresenceSource>? get_zone_sources (Presence zone) {
            if (zone.sources != null && zone.sources.length > 0) {
                return zone.sources;
            }

            if (zone.controller_id.length > 0) {
                var controller = node_repo.get_node (zone.controller_id);
                if (controller != null && controller.sources != null &&
                    controller.sources.length > 0) {
                    return controller.sources;
                }
            }

            var controllers = node_repo.get_zone_controllers ();
            if (controllers.length > 0 && controllers[0].sources != null &&
                controllers[0].sources.length > 0) {
                return controllers[0].sources;
            }

            return null;
        }

        private string? get_zone_controller_id (string zone_id) {
            var zone = node_repo.get_node (zone_id);
            if (zone != null && zone.controller_id.length > 0) {
                return zone.controller_id;
            }

            var controllers = node_repo.get_zone_controllers ();
            if (controllers.length > 0) {
                return controllers[0].node_id;
            }

            return null;
        }

        private string humanize_source_id (string source_id) {
            if (source_id.contains (":")) {
                return source_id.substring (source_id.last_index_of_char (':') + 1);
            }
            return source_id;
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
