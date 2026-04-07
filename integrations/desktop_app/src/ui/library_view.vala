/* library_view.vala — Mu.LibraryView : Adw.Bin
 * Dual-tab Library/Playlists view with hierarchical browsing, search,
 * and play/queue actions.
 */

namespace Mu {

    /* ---- Breadcrumb entry for navigation stack ---- */
    private class BrowseCrumb {
        public string container_id;
        public string label;

        public BrowseCrumb (string container_id, string label) {
            this.container_id = container_id;
            this.label = label;
        }
    }

    public class LibraryView : Adw.Bin {

        /* Service dependencies */
        private NodeRepository node_repo;
        private LibraryRepository library_repo;
        private PlaylistRepository playlist_repo;
        private ActiveRendererRepository active_repo;
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;
        private ArtworkLoader artwork_loader;

        /* Main layout */
        private Gtk.Stack tab_stack;

        /* ---- Libraries tab ---- */
        private Gtk.DropDown library_dropdown;
        private Gtk.Box library_dropdown_box;
        private Gtk.SearchEntry search_entry;
        private Gtk.Box breadcrumb_box;
        private Gtk.ListBox content_list;
        private Gtk.Box empty_box;
        private Gtk.Box toolbar_box;
        private Gtk.Button load_more_button;
        private Gtk.Spinner load_spinner;

        /* Library state */
        private GenericArray<Presence> libraries;
        private GLib.ListStore library_model;
        private GenericArray<BrowseCrumb> nav_stack;
        private GenericArray<Json.Object> current_items;
        private int64 browse_offset = 0;
        private bool has_more = false;
        private bool is_loading = false;
        private bool is_search_mode = false;
        private string search_query = "";
        private uint search_timeout_id = 0;
        private const int64 PAGE_SIZE = 50;

        /* ---- Playlists tab ---- */
        private Gtk.DropDown playlist_dropdown;
        private Gtk.Box playlist_dropdown_box;
        private Gtk.ListBox playlist_list;
        private Gtk.Box playlist_empty_box;
        private Gtk.Box playlist_content_box;
        private Gtk.ListBox playlist_track_list;
        private Gtk.Label playlist_title_label;
        private Gtk.Button playlist_back_button;
        private Gtk.Box playlist_toolbar_box;
        private Gtk.Spinner playlist_spinner;

        /* Playlist state */
        private GenericArray<Presence> playlist_servers;
        private GLib.ListStore playlist_server_model;
        private GenericArray<PlaylistSummary>? cached_playlists = null;
        private string? viewing_playlist_id = null;
        private string? viewing_playlist_name = null;

        /* Signal handler IDs */
        private ulong node_added_id = 0;
        private ulong node_removed_id = 0;

        public LibraryView (NodeRepository node_repo,
                            LibraryRepository library_repo,
                            PlaylistRepository playlist_repo,
                            ActiveRendererRepository active_repo,
                            CommandCorrelator correlator,
                            LeaseManager lease_mgr,
                            ArtworkLoader artwork_loader) {
            this.node_repo = node_repo;
            this.library_repo = library_repo;
            this.playlist_repo = playlist_repo;
            this.active_repo = active_repo;
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;
            this.artwork_loader = artwork_loader;

            this.libraries = new GenericArray<Presence> ();
            this.library_model = new GLib.ListStore (typeof (Gtk.StringObject));
            this.nav_stack = new GenericArray<BrowseCrumb> ();
            this.current_items = new GenericArray<Json.Object> ();

            this.playlist_servers = new GenericArray<Presence> ();
            this.playlist_server_model = new GLib.ListStore (typeof (Gtk.StringObject));

            build_ui ();
            connect_signals ();
            populate_initial ();
        }

        /* ================================================================
         * UI construction
         * ================================================================ */

        private void build_ui () {
            var scroll = new Gtk.ScrolledWindow ();
            scroll.hscrollbar_policy = Gtk.PolicyType.NEVER;
            scroll.vexpand = true;
            scroll.hexpand = true;

            var outer = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            outer.margin_start = 24;
            outer.margin_end = 24;
            outer.margin_top = 24;
            outer.margin_bottom = 24;

            /* Header */
            var title = new Gtk.Label ("Library");
            title.add_css_class ("section-title");
            title.halign = Gtk.Align.START;
            outer.append (title);

            var subtitle = new Gtk.Label ("Browse music and playlists");
            subtitle.add_css_class ("meta-label");
            subtitle.halign = Gtk.Align.START;
            subtitle.margin_top = 2;
            subtitle.margin_bottom = 16;
            outer.append (subtitle);

            /* Tab switcher */
            tab_stack = new Gtk.Stack ();
            tab_stack.transition_type = Gtk.StackTransitionType.CROSSFADE;
            tab_stack.transition_duration = 150;

            var switcher_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 4);
            switcher_box.halign = Gtk.Align.START;
            switcher_box.margin_bottom = 16;

            var lib_tab_btn = new Gtk.ToggleButton.with_label ("Libraries");
            lib_tab_btn.add_css_class ("queue-toggle-button");
            lib_tab_btn.active = true;
            lib_tab_btn.add_css_class ("queue-toggle-active");
            switcher_box.append (lib_tab_btn);

            var pl_tab_btn = new Gtk.ToggleButton.with_label ("Playlists");
            pl_tab_btn.add_css_class ("queue-toggle-button");
            pl_tab_btn.group = lib_tab_btn;
            switcher_box.append (pl_tab_btn);

            lib_tab_btn.toggled.connect (() => {
                if (lib_tab_btn.active) {
                    tab_stack.visible_child_name = "libraries";
                    lib_tab_btn.add_css_class ("queue-toggle-active");
                    pl_tab_btn.remove_css_class ("queue-toggle-active");
                }
            });

            pl_tab_btn.toggled.connect (() => {
                if (pl_tab_btn.active) {
                    tab_stack.visible_child_name = "playlists";
                    pl_tab_btn.add_css_class ("queue-toggle-active");
                    lib_tab_btn.remove_css_class ("queue-toggle-active");
                    /* Refresh playlists on tab switch */
                    if (cached_playlists == null) {
                        refresh_playlists ();
                    }
                }
            });

            outer.append (switcher_box);

            /* Build tab contents */
            tab_stack.add_named (build_libraries_tab (), "libraries");
            tab_stack.add_named (build_playlists_tab (), "playlists");
            tab_stack.visible_child_name = "libraries";

            outer.append (tab_stack);

            scroll.child = outer;
            this.child = scroll;
        }

        /* ---- Libraries Tab ---- */

        private Gtk.Widget build_libraries_tab () {
            var vbox = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);

            /* Library selector dropdown (hidden if 0 or 1 libraries) */
            library_dropdown_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            library_dropdown_box.margin_bottom = 12;
            library_dropdown_box.visible = false;

            var lib_label = new Gtk.Label ("Library:");
            lib_label.add_css_class ("heading-small");
            library_dropdown_box.append (lib_label);

            library_dropdown = new Gtk.DropDown (library_model, null);
            library_dropdown.hexpand = true;
            library_dropdown.notify["selected"].connect (on_library_selected);
            library_dropdown_box.append (library_dropdown);

            vbox.append (library_dropdown_box);

            /* Search bar */
            search_entry = new Gtk.SearchEntry ();
            search_entry.placeholder_text = "Search library...";
            search_entry.margin_bottom = 12;
            search_entry.search_changed.connect (on_search_changed);
            vbox.append (search_entry);

            /* Breadcrumb navigation */
            breadcrumb_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 0);
            breadcrumb_box.add_css_class ("library-breadcrumb");
            breadcrumb_box.margin_bottom = 8;
            vbox.append (breadcrumb_box);

            /* Play All / Queue All toolbar */
            toolbar_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            toolbar_box.margin_bottom = 12;
            toolbar_box.visible = false;

            var play_all_btn = new Gtk.Button.with_label ("Play All");
            play_all_btn.add_css_class ("suggested-action");
            play_all_btn.clicked.connect (on_play_all);
            toolbar_box.append (play_all_btn);

            var queue_all_btn = new Gtk.Button.with_label ("Queue All");
            queue_all_btn.add_css_class ("queue-toggle-button");
            queue_all_btn.clicked.connect (on_queue_all);
            toolbar_box.append (queue_all_btn);

            vbox.append (toolbar_box);

            /* Empty state */
            empty_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 12);
            empty_box.halign = Gtk.Align.CENTER;
            empty_box.valign = Gtk.Align.CENTER;
            empty_box.vexpand = true;
            empty_box.margin_top = 60;

            var empty_icon = new Gtk.Image.from_icon_name ("folder-music-symbolic");
            empty_icon.pixel_size = 48;
            empty_icon.add_css_class ("text-secondary");
            empty_box.append (empty_icon);

            var empty_title = new Gtk.Label ("No Libraries Found");
            empty_title.add_css_class ("heading-medium");
            empty_box.append (empty_title);

            var empty_sub = new Gtk.Label ("Waiting for library nodes to appear on the network");
            empty_sub.add_css_class ("text-secondary");
            empty_box.append (empty_sub);

            vbox.append (empty_box);

            /* Content list */
            content_list = new Gtk.ListBox ();
            content_list.selection_mode = Gtk.SelectionMode.NONE;
            content_list.add_css_class ("library-content-list");
            content_list.row_activated.connect (on_content_row_activated);
            vbox.append (content_list);

            /* Loading spinner */
            load_spinner = new Gtk.Spinner ();
            load_spinner.margin_top = 16;
            load_spinner.halign = Gtk.Align.CENTER;
            load_spinner.visible = false;
            vbox.append (load_spinner);

            /* Load more button */
            load_more_button = new Gtk.Button.with_label ("Load More");
            load_more_button.add_css_class ("queue-toggle-button");
            load_more_button.halign = Gtk.Align.CENTER;
            load_more_button.margin_top = 12;
            load_more_button.visible = false;
            load_more_button.clicked.connect (on_load_more);
            vbox.append (load_more_button);

            return vbox;
        }

        /* ---- Playlists Tab ---- */

        private Gtk.Widget build_playlists_tab () {
            var vbox = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);

            /* Playlist server selector */
            playlist_dropdown_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            playlist_dropdown_box.margin_bottom = 12;
            playlist_dropdown_box.visible = false;

            var pl_label = new Gtk.Label ("Server:");
            pl_label.add_css_class ("heading-small");
            playlist_dropdown_box.append (pl_label);

            playlist_dropdown = new Gtk.DropDown (playlist_server_model, null);
            playlist_dropdown.hexpand = true;
            playlist_dropdown.notify["selected"].connect (on_playlist_server_selected);
            playlist_dropdown_box.append (playlist_dropdown);

            vbox.append (playlist_dropdown_box);

            /* Playlist loading spinner */
            playlist_spinner = new Gtk.Spinner ();
            playlist_spinner.margin_top = 16;
            playlist_spinner.halign = Gtk.Align.CENTER;
            playlist_spinner.visible = false;
            vbox.append (playlist_spinner);

            /* Empty state for playlists */
            playlist_empty_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 12);
            playlist_empty_box.halign = Gtk.Align.CENTER;
            playlist_empty_box.valign = Gtk.Align.CENTER;
            playlist_empty_box.vexpand = true;
            playlist_empty_box.margin_top = 60;

            var pl_empty_icon = new Gtk.Image.from_icon_name ("view-list-symbolic");
            pl_empty_icon.pixel_size = 48;
            pl_empty_icon.add_css_class ("text-secondary");
            playlist_empty_box.append (pl_empty_icon);

            var pl_empty_title = new Gtk.Label ("No Playlists");
            pl_empty_title.add_css_class ("heading-medium");
            playlist_empty_box.append (pl_empty_title);

            var pl_empty_sub = new Gtk.Label ("Waiting for playlist servers on the network");
            pl_empty_sub.add_css_class ("text-secondary");
            playlist_empty_box.append (pl_empty_sub);

            vbox.append (playlist_empty_box);

            /* Playlist list (shown when browsing playlists) */
            playlist_list = new Gtk.ListBox ();
            playlist_list.selection_mode = Gtk.SelectionMode.NONE;
            playlist_list.add_css_class ("library-content-list");
            playlist_list.row_activated.connect (on_playlist_row_activated);
            playlist_list.visible = false;
            vbox.append (playlist_list);

            /* Playlist content view (shown when viewing a specific playlist) */
            playlist_content_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            playlist_content_box.visible = false;

            /* Back button + playlist name */
            var pl_header = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            pl_header.margin_bottom = 12;

            playlist_back_button = new Gtk.Button.from_icon_name ("go-previous-symbolic");
            playlist_back_button.add_css_class ("queue-toggle-button");
            playlist_back_button.tooltip_text = "Back to playlists";
            playlist_back_button.clicked.connect (on_playlist_back);
            pl_header.append (playlist_back_button);

            playlist_title_label = new Gtk.Label ("");
            playlist_title_label.add_css_class ("heading-medium");
            playlist_title_label.halign = Gtk.Align.START;
            playlist_title_label.hexpand = true;
            playlist_title_label.ellipsize = Pango.EllipsizeMode.END;
            pl_header.append (playlist_title_label);

            playlist_content_box.append (pl_header);

            /* Playlist action toolbar */
            playlist_toolbar_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 8);
            playlist_toolbar_box.margin_bottom = 12;

            var load_pl_btn = new Gtk.Button.with_label ("Load Playlist");
            load_pl_btn.add_css_class ("suggested-action");
            load_pl_btn.clicked.connect (() => on_load_playlist ("replace"));
            playlist_toolbar_box.append (load_pl_btn);

            var append_pl_btn = new Gtk.Button.with_label ("Append");
            append_pl_btn.add_css_class ("queue-toggle-button");
            append_pl_btn.clicked.connect (() => on_load_playlist ("append"));
            playlist_toolbar_box.append (append_pl_btn);

            playlist_content_box.append (playlist_toolbar_box);

            /* Track list within playlist */
            playlist_track_list = new Gtk.ListBox ();
            playlist_track_list.selection_mode = Gtk.SelectionMode.NONE;
            playlist_track_list.add_css_class ("library-content-list");
            playlist_content_box.append (playlist_track_list);

            vbox.append (playlist_content_box);

            return vbox;
        }

        /* ================================================================
         * Signal wiring
         * ================================================================ */

        private void connect_signals () {
            node_added_id = node_repo.node_added.connect (on_node_added);
            node_removed_id = node_repo.node_removed.connect (on_node_removed);
        }

        private void populate_initial () {
            /* Populate libraries */
            var libs = node_repo.get_libraries ();
            for (uint i = 0; i < libs.length; i++) {
                add_library_node (libs[i]);
            }
            update_library_dropdown ();

            /* Auto-browse root if we have a library */
            if (libraries.length > 0) {
                browse_root ();
            }

            /* Populate playlist servers */
            var pls = node_repo.get_playlist_servers ();
            for (uint i = 0; i < pls.length; i++) {
                add_playlist_server_node (pls[i]);
            }
            update_playlist_dropdown ();
        }

        private void on_node_added (Presence presence) {
            if (presence.kind == "library") {
                if (!has_library (presence.node_id)) {
                    add_library_node (presence);
                    update_library_dropdown ();
                    if (libraries.length == 1) {
                        browse_root ();
                    }
                }
            } else if (presence.kind == "playlist") {
                if (!has_playlist_server (presence.node_id)) {
                    add_playlist_server_node (presence);
                    update_playlist_dropdown ();
                }
            }
        }

        private void on_node_removed (string node_id) {
            /* Check if it was a library */
            for (uint i = 0; i < libraries.length; i++) {
                if (libraries[i].node_id == node_id) {
                    libraries.remove_index (i);
                    update_library_dropdown ();
                    if (libraries.length == 0) {
                        clear_library_content ();
                    }
                    return;
                }
            }

            /* Check if it was a playlist server */
            for (uint i = 0; i < playlist_servers.length; i++) {
                if (playlist_servers[i].node_id == node_id) {
                    playlist_servers.remove_index (i);
                    update_playlist_dropdown ();
                    if (playlist_servers.length == 0) {
                        clear_playlist_content ();
                    }
                    return;
                }
            }
        }

        /* ================================================================
         * Library node management
         * ================================================================ */

        private bool has_library (string node_id) {
            for (uint i = 0; i < libraries.length; i++) {
                if (libraries[i].node_id == node_id) return true;
            }
            return false;
        }

        private void add_library_node (Presence p) {
            libraries.add (p);
        }

        private void update_library_dropdown () {
            library_model.remove_all ();
            for (uint i = 0; i < libraries.length; i++) {
                library_model.append (new Gtk.StringObject (libraries[i].name));
            }

            library_dropdown_box.visible = (libraries.length > 1);
            empty_box.visible = (libraries.length == 0 && current_items.length == 0);
        }

        private string? get_selected_library_id () {
            if (libraries.length == 0) return null;
            var idx = library_dropdown.selected;
            if (idx == Gtk.INVALID_LIST_POSITION || idx >= libraries.length) {
                return libraries[0].node_id;
            }
            return libraries[idx].node_id;
        }

        private void on_library_selected () {
            /* Reset browse state and re-browse root */
            nav_stack = new GenericArray<BrowseCrumb> ();
            browse_offset = 0;
            is_search_mode = false;
            search_entry.text = "";
            browse_root ();
        }

        /* ================================================================
         * Playlist server management
         * ================================================================ */

        private bool has_playlist_server (string node_id) {
            for (uint i = 0; i < playlist_servers.length; i++) {
                if (playlist_servers[i].node_id == node_id) return true;
            }
            return false;
        }

        private void add_playlist_server_node (Presence p) {
            playlist_servers.add (p);
        }

        private void update_playlist_dropdown () {
            playlist_server_model.remove_all ();
            for (uint i = 0; i < playlist_servers.length; i++) {
                playlist_server_model.append (
                    new Gtk.StringObject (playlist_servers[i].name));
            }

            playlist_dropdown_box.visible = (playlist_servers.length > 1);
            playlist_empty_box.visible = (playlist_servers.length == 0);
        }

        private string? get_selected_playlist_server_id () {
            if (playlist_servers.length == 0) return null;
            var idx = playlist_dropdown.selected;
            if (idx == Gtk.INVALID_LIST_POSITION || idx >= playlist_servers.length) {
                return playlist_servers[0].node_id;
            }
            return playlist_servers[idx].node_id;
        }

        private void on_playlist_server_selected () {
            cached_playlists = null;
            viewing_playlist_id = null;
            refresh_playlists ();
        }

        /* ================================================================
         * Library browsing
         * ================================================================ */

        private void browse_root () {
            nav_stack = new GenericArray<BrowseCrumb> ();
            nav_stack.add (new BrowseCrumb ("", "Root"));
            browse_offset = 0;
            is_search_mode = false;
            load_container ("", true);
        }

        private void navigate_to (string container_id, string label) {
            is_search_mode = false;
            search_entry.text = "";
            nav_stack.add (new BrowseCrumb (container_id, label));
            browse_offset = 0;
            load_container (container_id, true);
        }

        private void navigate_back_to (int crumb_index) {
            if (crumb_index < 0 || crumb_index >= (int) nav_stack.length) return;

            /* Trim nav stack to the clicked crumb */
            var new_stack = new GenericArray<BrowseCrumb> ();
            for (int i = 0; i <= crumb_index; i++) {
                new_stack.add (nav_stack[i]);
            }
            nav_stack = new_stack;

            is_search_mode = false;
            search_entry.text = "";
            browse_offset = 0;

            var crumb = nav_stack[crumb_index];
            load_container (crumb.container_id, true);
        }

        private void load_container (string container_id, bool replace) {
            var library_id = get_selected_library_id ();
            if (library_id == null) return;

            if (is_loading) return;
            is_loading = true;

            load_spinner.visible = true;
            load_spinner.spinning = true;
            load_more_button.visible = false;

            library_repo.browse.begin (
                library_id, container_id, browse_offset, PAGE_SIZE,
                (obj, res) => {
                    var items = library_repo.browse.end (res);
                    is_loading = false;
                    load_spinner.visible = false;
                    load_spinner.spinning = false;

                    if (items == null) {
                        if (replace) {
                            current_items = new GenericArray<Json.Object> ();
                            rebuild_content_list ();
                        }
                        return;
                    }

                    if (replace) {
                        current_items = new GenericArray<Json.Object> ();
                    }

                    for (uint i = 0; i < items.get_length (); i++) {
                        current_items.add (items.get_object_element (i));
                    }

                    has_more = (items.get_length () >= (uint) PAGE_SIZE);
                    browse_offset += (int64) items.get_length ();

                    rebuild_content_list ();
                }
            );
        }

        private void on_load_more () {
            if (is_search_mode) {
                do_search (false);
            } else if (nav_stack.length > 0) {
                var crumb = nav_stack[nav_stack.length - 1];
                load_container (crumb.container_id, false);
            }
        }

        /* ================================================================
         * Search
         * ================================================================ */

        private void on_search_changed () {
            /* Cancel existing debounce timeout */
            if (search_timeout_id != 0) {
                Source.remove (search_timeout_id);
                search_timeout_id = 0;
            }

            var query = search_entry.text.strip ();

            if (query.length == 0) {
                /* Clear search, return to browse mode */
                is_search_mode = false;
                search_query = "";
                browse_offset = 0;
                if (nav_stack.length > 0) {
                    var crumb = nav_stack[nav_stack.length - 1];
                    load_container (crumb.container_id, true);
                }
                return;
            }

            /* 300ms debounce */
            search_timeout_id = Timeout.add (300, () => {
                search_timeout_id = 0;
                search_query = query;
                is_search_mode = true;
                browse_offset = 0;
                do_search (true);
                return Source.REMOVE;
            });
        }

        private void do_search (bool replace) {
            var library_id = get_selected_library_id ();
            if (library_id == null) return;

            if (is_loading) return;
            is_loading = true;

            load_spinner.visible = true;
            load_spinner.spinning = true;
            load_more_button.visible = false;

            library_repo.search.begin (
                library_id, search_query, browse_offset, PAGE_SIZE,
                (obj, res) => {
                    var items = library_repo.search.end (res);
                    is_loading = false;
                    load_spinner.visible = false;
                    load_spinner.spinning = false;

                    if (items == null) {
                        if (replace) {
                            current_items = new GenericArray<Json.Object> ();
                            rebuild_content_list ();
                        }
                        return;
                    }

                    if (replace) {
                        current_items = new GenericArray<Json.Object> ();
                    }

                    for (uint i = 0; i < items.get_length (); i++) {
                        current_items.add (items.get_object_element (i));
                    }

                    has_more = (items.get_length () >= (uint) PAGE_SIZE);
                    browse_offset += (int64) items.get_length ();

                    rebuild_content_list ();
                }
            );
        }

        /* ================================================================
         * Content list management
         * ================================================================ */

        private void rebuild_content_list () {
            /* Clear existing rows */
            Gtk.Widget? child = content_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                content_list.remove (child);
                child = next;
            }

            rebuild_breadcrumbs ();

            if (current_items.length == 0) {
                empty_box.visible = true;
                content_list.visible = false;
                toolbar_box.visible = false;
                load_more_button.visible = false;

                /* Customize empty message */
                var empty_title = empty_box.get_first_child ();
                if (empty_title != null) empty_title = empty_title.get_next_sibling ();
                if (empty_title != null) {
                    var label = empty_title as Gtk.Label;
                    if (label != null) {
                        if (is_search_mode) {
                            label.label = "No Results";
                        } else if (libraries.length == 0) {
                            label.label = "No Libraries Found";
                        } else {
                            label.label = "Empty";
                        }
                    }
                }
                return;
            }

            empty_box.visible = false;
            content_list.visible = true;

            /* Show toolbar if we have tracks in this container */
            var has_tracks = false;
            for (uint i = 0; i < current_items.length; i++) {
                if (!is_container_item (current_items[i])) {
                    has_tracks = true;
                    break;
                }
            }
            toolbar_box.visible = has_tracks && !is_search_mode;

            /* Build rows */
            for (uint i = 0; i < current_items.length; i++) {
                var row = build_content_row ((int) i, current_items[i]);
                content_list.append (row);
            }

            /* Load more button */
            load_more_button.visible = has_more;
        }

        private void rebuild_breadcrumbs () {
            /* Clear existing crumbs */
            Gtk.Widget? child = breadcrumb_box.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                breadcrumb_box.remove (child);
                child = next;
            }

            if (is_search_mode || nav_stack.length <= 1) {
                breadcrumb_box.visible = false;
                return;
            }

            breadcrumb_box.visible = true;

            for (int i = 0; i < (int) nav_stack.length; i++) {
                if (i > 0) {
                    var sep = new Gtk.Label (" \u203A ");
                    sep.add_css_class ("text-secondary");
                    breadcrumb_box.append (sep);
                }

                var crumb = nav_stack[i];
                var crumb_label = (i == 0) ? "Home" : crumb.label;

                if (i < (int) nav_stack.length - 1) {
                    /* Clickable crumb */
                    var btn = new Gtk.Button.with_label (crumb_label);
                    btn.add_css_class ("flat");
                    btn.add_css_class ("library-breadcrumb-link");
                    var crumb_index = i;  /* capture for closure */
                    btn.clicked.connect (() => {
                        navigate_back_to (crumb_index);
                    });
                    breadcrumb_box.append (btn);
                } else {
                    /* Current (non-clickable) */
                    var label = new Gtk.Label (crumb_label);
                    label.add_css_class ("heading-small");
                    breadcrumb_box.append (label);
                }
            }
        }

        private Gtk.ListBoxRow build_content_row (int index, Json.Object item) {
            var row = new Gtk.ListBoxRow ();
            row.name = index.to_string ();
            row.focusable = true;
            row.activatable = true;

            var card = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            card.add_css_class ("mu-card");
            card.add_css_class ("library-row");
            card.margin_top = 2;
            card.margin_bottom = 2;

            var is_container = is_container_item (item);

            if (is_container) {
                /* Container row: folder icon + name + chevron */
                var icon = new Gtk.Image.from_icon_name ("folder-music-symbolic");
                icon.pixel_size = 24;
                icon.add_css_class ("text-secondary");
                icon.valign = Gtk.Align.CENTER;
                card.append (icon);

                var name_label = new Gtk.Label (get_item_title (item));
                name_label.add_css_class ("queue-track-title");
                name_label.halign = Gtk.Align.START;
                name_label.hexpand = true;
                name_label.ellipsize = Pango.EllipsizeMode.END;
                name_label.max_width_chars = 60;
                name_label.valign = Gtk.Align.CENTER;
                card.append (name_label);

                /* Child count if available */
                var child_count = get_item_child_count (item);
                if (child_count > 0) {
                    var count_label = new Gtk.Label ("%ld items".printf ((long) child_count));
                    count_label.add_css_class ("text-secondary");
                    count_label.add_css_class ("caption");
                    count_label.valign = Gtk.Align.CENTER;
                    card.append (count_label);
                }

                var chevron = new Gtk.Image.from_icon_name ("go-next-symbolic");
                chevron.pixel_size = 16;
                chevron.add_css_class ("text-secondary");
                chevron.valign = Gtk.Align.CENTER;
                card.append (chevron);
            } else {
                /* Track row: artwork + title/artist + duration + action buttons */
                var art = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
                art.add_css_class ("queue-art");
                art.width_request = 40;
                art.height_request = 40;

                var art_icon = new Gtk.Image.from_icon_name ("media-optical-symbolic");
                art_icon.pixel_size = 20;
                art_icon.add_css_class ("text-secondary");
                art_icon.halign = Gtk.Align.CENTER;
                art_icon.valign = Gtk.Align.CENTER;
                art_icon.hexpand = true;
                art_icon.vexpand = true;
                art.append (art_icon);

                var art_picture = new Gtk.Picture ();
                art_picture.content_fit = Gtk.ContentFit.COVER;
                art_picture.width_request = 40;
                art_picture.height_request = 40;
                art_picture.visible = false;
                art.append (art_picture);

                /* Load artwork from metadata */
                var art_url = get_item_artwork_url (item);
                if (art_url.length > 0) {
                    artwork_loader.load_async (art_url, (texture) => {
                        if (texture != null && art_picture.get_parent () != null) {
                            art_picture.paintable = texture;
                            art_picture.visible = true;
                            art_icon.visible = false;
                        }
                    });
                }

                card.append (art);

                /* Text */
                var text_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 2);
                text_box.hexpand = true;
                text_box.valign = Gtk.Align.CENTER;

                var title_label = new Gtk.Label (get_item_title (item));
                title_label.add_css_class ("queue-track-title");
                title_label.halign = Gtk.Align.START;
                title_label.ellipsize = Pango.EllipsizeMode.END;
                title_label.max_width_chars = 50;
                text_box.append (title_label);

                var artist = get_item_artist (item);
                if (artist.length > 0) {
                    var artist_label = new Gtk.Label (artist);
                    artist_label.add_css_class ("queue-track-artist");
                    artist_label.halign = Gtk.Align.START;
                    artist_label.ellipsize = Pango.EllipsizeMode.END;
                    artist_label.max_width_chars = 50;
                    text_box.append (artist_label);
                }

                card.append (text_box);

                /* Duration */
                var duration_ms = get_item_duration_ms (item);
                if (duration_ms > 0) {
                    var dur_label = new Gtk.Label (format_duration (duration_ms));
                    dur_label.add_css_class ("text-timestamp");
                    dur_label.valign = Gtk.Align.CENTER;
                    card.append (dur_label);
                }

                /* Action buttons */
                var play_btn = new Gtk.Button.from_icon_name ("media-playback-start-symbolic");
                play_btn.add_css_class ("library-action-button");
                play_btn.valign = Gtk.Align.CENTER;
                play_btn.tooltip_text = "Play";
                var item_idx = index;
                play_btn.clicked.connect (() => {
                    on_play_item (item_idx);
                });
                card.append (play_btn);

                var queue_btn = new Gtk.Button.from_icon_name ("list-add-symbolic");
                queue_btn.add_css_class ("library-action-button");
                queue_btn.valign = Gtk.Align.CENTER;
                queue_btn.tooltip_text = "Add to queue";
                queue_btn.clicked.connect (() => {
                    on_queue_item (item_idx);
                });
                card.append (queue_btn);
            }

            row.child = card;
            return row;
        }

        private void on_content_row_activated (Gtk.ListBoxRow row) {
            var index_str = row.name;
            if (index_str == null || index_str.length == 0) return;

            var index = int.parse (index_str);
            if (index < 0 || index >= (int) current_items.length) return;

            var item = current_items[index];

            if (is_container_item (item)) {
                /* Navigate into the container */
                var item_id = get_item_id (item);
                var title = get_item_title (item);
                navigate_to (item_id, title);
            } else {
                /* Play the track */
                on_play_item (index);
            }
        }

        private void clear_library_content () {
            current_items = new GenericArray<Json.Object> ();
            nav_stack = new GenericArray<BrowseCrumb> ();
            browse_offset = 0;
            rebuild_content_list ();
        }

        /* ================================================================
         * Play / Queue actions (Library)
         * ================================================================ */

        private void on_play_item (int index) {
            if (index < 0 || index >= (int) current_items.length) return;

            var item = current_items[index];
            var item_id = get_item_id (item);
            var library_id = get_selected_library_id ();
            var renderer_id = active_repo.active_renderer_id;

            if (library_id == null || renderer_id.length == 0) return;

            /* Resolve, then queue.add at "next", then playback.play */
            library_repo.resolve.begin (library_id, item_id, false, (obj, res) => {
                var resolved = library_repo.resolve.end (res);
                if (resolved == null) return;

                var entry = build_queue_entry_from_resolved (item_id, item, resolved);
                if (entry == null) return;

                var entries = new Json.Array ();
                entries.add_object_element (entry);

                lease_mgr.ensure_lease.begin (renderer_id, (lobj, lres) => {
                    var lease = lease_mgr.ensure_lease.end (lres);
                    if (lease == null) return;

                    var add_body = QueueBodies.add ("next", entries);
                    correlator.send.begin (
                        renderer_id, "queue.add", add_body, lease, -1, 3000,
                        (aobj, ares) => {
                            var add_reply = correlator.send.end (ares);
                            if (add_reply == null || !add_reply.ok) return;

                            /* Get the index to play: the queue state tells us
                             * current index, and "next" inserts at current+1 */
                            int64 play_index = 0;
                            if (add_reply.body != null &&
                                add_reply.body.has_member ("index")) {
                                play_index = add_reply.body.get_int_member ("index");
                            }

                            correlator.send_fire_and_forget (
                                renderer_id, "playback.play",
                                PlaybackBodies.play (play_index), lease);
                        }
                    );
                });
            });
        }

        private void on_queue_item (int index) {
            if (index < 0 || index >= (int) current_items.length) return;

            var item = current_items[index];
            var item_id = get_item_id (item);
            var library_id = get_selected_library_id ();
            var renderer_id = active_repo.active_renderer_id;

            if (library_id == null || renderer_id.length == 0) return;

            /* Resolve, then queue.add at "end" */
            library_repo.resolve.begin (library_id, item_id, false, (obj, res) => {
                var resolved = library_repo.resolve.end (res);
                if (resolved == null) return;

                var entry = build_queue_entry_from_resolved (item_id, item, resolved);
                if (entry == null) return;

                var entries = new Json.Array ();
                entries.add_object_element (entry);

                lease_mgr.ensure_lease.begin (renderer_id, (lobj, lres) => {
                    var lease = lease_mgr.ensure_lease.end (lres);
                    if (lease == null) return;

                    var add_body = QueueBodies.add ("end", entries);
                    correlator.send_fire_and_forget (
                        renderer_id, "queue.add", add_body, lease);
                });
            });
        }

        private void on_play_all () {
            var renderer_id = active_repo.active_renderer_id;
            var library_id = get_selected_library_id ();
            if (library_id == null || renderer_id.length == 0) return;

            /* Collect all track item IDs from current_items */
            var track_ids = new GenericArray<string> ();
            var track_items = new GenericArray<Json.Object> ();
            for (uint i = 0; i < current_items.length; i++) {
                if (!is_container_item (current_items[i])) {
                    track_ids.add (get_item_id (current_items[i]));
                    track_items.add (current_items[i]);
                }
            }

            if (track_ids.length == 0) return;

            /* Resolve all tracks in batch */
            var id_array = new string[track_ids.length];
            for (uint i = 0; i < track_ids.length; i++) {
                id_array[i] = track_ids[i];
            }

            library_repo.resolve_batch.begin (
                library_id, id_array, false,
                (obj, res) => {
                    var resolved_list = library_repo.resolve_batch.end (res);
                    if (resolved_list == null || resolved_list.length == 0) return;

                    var entries = new Json.Array ();
                    for (uint i = 0; i < resolved_list.length; i++) {
                        var item_id = (i < track_ids.length) ? track_ids[i] : "";
                        var browse_item = (i < track_items.length) ? track_items[i] : null;
                        var entry = build_queue_entry_from_batch_resolved (
                            item_id, browse_item, resolved_list[i]);
                        if (entry != null) {
                            entries.add_object_element (entry);
                        }
                    }

                    if (entries.get_length () == 0) return;

                    /* Replace queue and play from start */
                    lease_mgr.ensure_lease.begin (renderer_id, (lobj, lres) => {
                        var lease = lease_mgr.ensure_lease.end (lres);
                        if (lease == null) return;

                        var set_body = QueueBodies.set_entries (0, entries);
                        correlator.send.begin (
                            renderer_id, "queue.set", set_body, lease, -1, 5000,
                            (sobj, sres) => {
                                var set_reply = correlator.send.end (sres);
                                if (set_reply == null || !set_reply.ok) return;

                                correlator.send_fire_and_forget (
                                    renderer_id, "playback.play",
                                    PlaybackBodies.play (0), lease);
                            }
                        );
                    });
                }
            );
        }

        private void on_queue_all () {
            var renderer_id = active_repo.active_renderer_id;
            var library_id = get_selected_library_id ();
            if (library_id == null || renderer_id.length == 0) return;

            /* Collect all track item IDs from current_items */
            var track_ids = new GenericArray<string> ();
            var track_items = new GenericArray<Json.Object> ();
            for (uint i = 0; i < current_items.length; i++) {
                if (!is_container_item (current_items[i])) {
                    track_ids.add (get_item_id (current_items[i]));
                    track_items.add (current_items[i]);
                }
            }

            if (track_ids.length == 0) return;

            var id_array = new string[track_ids.length];
            for (uint i = 0; i < track_ids.length; i++) {
                id_array[i] = track_ids[i];
            }

            library_repo.resolve_batch.begin (
                library_id, id_array, false,
                (obj, res) => {
                    var resolved_list = library_repo.resolve_batch.end (res);
                    if (resolved_list == null || resolved_list.length == 0) return;

                    var entries = new Json.Array ();
                    for (uint i = 0; i < resolved_list.length; i++) {
                        var item_id = (i < track_ids.length) ? track_ids[i] : "";
                        var browse_item = (i < track_items.length) ? track_items[i] : null;
                        var entry = build_queue_entry_from_batch_resolved (
                            item_id, browse_item, resolved_list[i]);
                        if (entry != null) {
                            entries.add_object_element (entry);
                        }
                    }

                    if (entries.get_length () == 0) return;

                    lease_mgr.ensure_lease.begin (renderer_id, (lobj, lres) => {
                        var lease = lease_mgr.ensure_lease.end (lres);
                        if (lease == null) return;

                        var add_body = QueueBodies.add ("end", entries);
                        correlator.send_fire_and_forget (
                            renderer_id, "queue.add", add_body, lease);
                    });
                }
            );
        }

        /* ================================================================
         * Playlists
         * ================================================================ */

        private void refresh_playlists () {
            var server_id = get_selected_playlist_server_id ();
            if (server_id == null) return;

            playlist_spinner.visible = true;
            playlist_spinner.spinning = true;
            playlist_list.visible = false;
            playlist_content_box.visible = false;
            playlist_empty_box.visible = false;
            viewing_playlist_id = null;

            playlist_repo.list_playlists.begin (server_id, "", (obj, res) => {
                var playlists = playlist_repo.list_playlists.end (res);
                playlist_spinner.visible = false;
                playlist_spinner.spinning = false;

                if (playlists == null || playlists.length == 0) {
                    cached_playlists = null;
                    playlist_empty_box.visible = true;
                    playlist_list.visible = false;
                    return;
                }

                cached_playlists = playlists;
                rebuild_playlist_list ();
            });
        }

        private void rebuild_playlist_list () {
            /* Clear existing rows */
            Gtk.Widget? child = playlist_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                playlist_list.remove (child);
                child = next;
            }

            if (cached_playlists == null || cached_playlists.length == 0) {
                playlist_empty_box.visible = true;
                playlist_list.visible = false;
                return;
            }

            playlist_empty_box.visible = false;
            playlist_list.visible = true;
            playlist_content_box.visible = false;

            for (uint i = 0; i < cached_playlists.length; i++) {
                var row = build_playlist_row ((int) i, cached_playlists[i]);
                playlist_list.append (row);
            }
        }

        private Gtk.ListBoxRow build_playlist_row (int index, PlaylistSummary summary) {
            var row = new Gtk.ListBoxRow ();
            row.name = index.to_string ();
            row.focusable = true;
            row.activatable = true;

            var card = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            card.add_css_class ("mu-card");
            card.add_css_class ("library-row");
            card.margin_top = 2;
            card.margin_bottom = 2;

            var icon = new Gtk.Image.from_icon_name ("view-list-symbolic");
            icon.pixel_size = 24;
            icon.add_css_class ("text-secondary");
            icon.valign = Gtk.Align.CENTER;
            card.append (icon);

            var name_label = new Gtk.Label (summary.name);
            name_label.add_css_class ("queue-track-title");
            name_label.halign = Gtk.Align.START;
            name_label.hexpand = true;
            name_label.ellipsize = Pango.EllipsizeMode.END;
            name_label.max_width_chars = 60;
            name_label.valign = Gtk.Align.CENTER;
            card.append (name_label);

            var chevron = new Gtk.Image.from_icon_name ("go-next-symbolic");
            chevron.pixel_size = 16;
            chevron.add_css_class ("text-secondary");
            chevron.valign = Gtk.Align.CENTER;
            card.append (chevron);

            row.child = card;
            return row;
        }

        private void on_playlist_row_activated (Gtk.ListBoxRow row) {
            var index_str = row.name;
            if (index_str == null || index_str.length == 0) return;

            var index = int.parse (index_str);
            if (cached_playlists == null || index < 0 ||
                index >= (int) cached_playlists.length) return;

            var summary = cached_playlists[index];
            view_playlist (summary.playlist_id, summary.name);
        }

        private void view_playlist (string playlist_id, string name) {
            var server_id = get_selected_playlist_server_id ();
            if (server_id == null) return;

            viewing_playlist_id = playlist_id;
            viewing_playlist_name = name;
            playlist_title_label.label = name;

            playlist_list.visible = false;
            playlist_empty_box.visible = false;
            playlist_content_box.visible = true;

            playlist_spinner.visible = true;
            playlist_spinner.spinning = true;

            /* Clear existing tracks */
            Gtk.Widget? child = playlist_track_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                playlist_track_list.remove (child);
                child = next;
            }

            do_view_playlist.begin (server_id, playlist_id);
        }

        /**
         * Async playlist content loading: fetch entries, resolve metadata, display.
         */
        private async void do_view_playlist (string server_id, string playlist_id) {
            var entries = yield playlist_repo.get_playlist (server_id, playlist_id);

            playlist_spinner.visible = false;
            playlist_spinner.spinning = false;

            if (entries == null || entries.get_length () == 0) return;

            /* Parse lib: refs to extract library ID and raw item ID */
            var full_to_raw = new HashTable<string, string> (str_hash, str_equal);
            var lib_to_raw_ids = new HashTable<string, GenericArray<string>> (str_hash, str_equal);

            for (uint i = 0; i < entries.get_length (); i++) {
                var entry = entries.get_object_element (i);
                if (entry.has_member ("metadata") && !entry.get_null_member ("metadata")) continue;

                var full_id = get_playlist_entry_item_id (entry);
                if (full_id.length == 0) continue;

                string lib_id, raw_id;
                if (split_lib_ref (full_id, out lib_id, out raw_id)) {
                    full_to_raw.insert (full_id, raw_id);

                    var items = lib_to_raw_ids.lookup (lib_id);
                    if (items == null) {
                        items = new GenericArray<string> ();
                        lib_to_raw_ids.insert (lib_id, items);
                    }
                    items.add (raw_id);
                }
            }

            /* Resolve metadata per library */
            var lib_keys = new GenericArray<string> ();
            lib_to_raw_ids.foreach ((k, v) => { lib_keys.add (k); });

            for (uint li = 0; li < lib_keys.length; li++) {
                var lib_id = lib_keys[li];
                var raw_ids = lib_to_raw_ids.lookup (lib_id);
                if (raw_ids == null || raw_ids.length == 0) continue;

                var id_array = new string[raw_ids.length];
                for (uint k = 0; k < raw_ids.length; k++) {
                    id_array[k] = raw_ids[k];
                }
                yield library_repo.resolve_batch (lib_id, id_array, true);
            }

            /* Build track rows with resolved metadata */
            populate_playlist_track_rows (entries, full_to_raw);
        }

        private void populate_playlist_track_rows (Json.Array entries, HashTable<string, string>? full_to_raw = null) {
            /* Clear existing */
            Gtk.Widget? child = playlist_track_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                playlist_track_list.remove (child);
                child = next;
            }

            for (uint i = 0; i < entries.get_length (); i++) {
                var entry = entries.get_object_element (i);

                /* Merge cached metadata into entry if it has none */
                var full_id = get_playlist_entry_item_id (entry);
                if (full_id.length > 0 &&
                    !(entry.has_member ("metadata") && !entry.get_null_member ("metadata"))) {
                    /* Try raw item ID first (what resolve_batch caches under),
                     * then fall back to full lib: ID */
                    string? lookup_id = null;
                    if (full_to_raw != null) {
                        lookup_id = full_to_raw.lookup (full_id);
                    }
                    var cached = library_repo.get_cached_metadata (lookup_id ?? full_id);
                    if (cached == null && lookup_id != null) {
                        cached = library_repo.get_cached_metadata (full_id);
                    }
                    if (cached != null) {
                        entry.set_object_member ("metadata", cached);
                    }
                }

                var track_row = build_playlist_track_row ((int) i, entry);
                playlist_track_list.append (track_row);
            }
        }

        private string get_playlist_entry_item_id (Json.Object entry) {
            /* Entry may have itemId directly, or ref.id */
            if (entry.has_member ("itemId")) {
                var id = entry.get_string_member ("itemId");
                if (id != null && id.length > 0) return id;
            }
            if (entry.has_member ("ref") && !entry.get_null_member ("ref")) {
                var ref_obj = entry.get_object_member ("ref");
                if (ref_obj.has_member ("id")) {
                    return ref_obj.get_string_member ("id");
                }
            }
            return "";
        }

        /**
         * Split a lib: reference into (library_node_id, raw_item_id).
         * Format: lib:mu:library:{provider}:{namespace}:{resource}:{item_id}
         * Returns false if not a valid lib: reference.
         */
        private bool split_lib_ref (string ref_str, out string library_id, out string raw_item_id) {
            library_id = "";
            raw_item_id = "";
            if (!ref_str.has_prefix ("lib:")) return false;

            var rest = ref_str.substring (4); // strip "lib:"
            var parts = rest.split (":", 6);  // at most 6 parts
            if (parts.length < 6) return false;

            // First 5 parts = library node ID, 6th = raw item ID
            library_id = string.join (":", parts[0], parts[1], parts[2], parts[3], parts[4]);
            raw_item_id = parts[5];
            return library_id.length > 0 && raw_item_id.length > 0;
        }

        private string? get_first_library_id () {
            var libraries = node_repo.get_libraries ();
            if (libraries.length > 0) {
                return libraries[0].node_id;
            }
            return null;
        }

        private Gtk.ListBoxRow build_playlist_track_row (int index, Json.Object entry) {
            var row = new Gtk.ListBoxRow ();
            row.name = index.to_string ();
            row.activatable = false;

            var card = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            card.add_css_class ("mu-card");
            card.add_css_class ("library-row");
            card.margin_top = 2;
            card.margin_bottom = 2;

            /* Index number */
            var index_label = new Gtk.Label ("%02d".printf (index + 1));
            index_label.add_css_class ("queue-index");
            index_label.add_css_class ("text-secondary");
            index_label.width_request = 28;
            index_label.halign = Gtk.Align.CENTER;
            card.append (index_label);

            /* Artwork thumbnail */
            var art = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            art.add_css_class ("queue-art");
            art.width_request = 40;
            art.height_request = 40;

            var art_icon = new Gtk.Image.from_icon_name ("media-optical-symbolic");
            art_icon.pixel_size = 20;
            art_icon.add_css_class ("text-secondary");
            art_icon.halign = Gtk.Align.CENTER;
            art_icon.valign = Gtk.Align.CENTER;
            art_icon.hexpand = true;
            art_icon.vexpand = true;
            art.append (art_icon);

            var art_picture = new Gtk.Picture ();
            art_picture.content_fit = Gtk.ContentFit.COVER;
            art_picture.width_request = 40;
            art_picture.height_request = 40;
            art_picture.visible = false;
            art.append (art_picture);

            /* Load artwork from entry metadata */
            var art_url = get_item_artwork_url (entry);
            if (art_url.length > 0) {
                artwork_loader.load_async (art_url, (texture) => {
                    if (texture != null && art_picture.get_parent () != null) {
                        art_picture.paintable = texture;
                        art_picture.visible = true;
                        art_icon.visible = false;
                    }
                });
            }

            card.append (art);

            /* Track info from entry metadata */
            var text_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 2);
            text_box.hexpand = true;
            text_box.valign = Gtk.Align.CENTER;

            var track_title = get_json_string (entry, "title", "Unknown Track");
            if (entry.has_member ("metadata") && !entry.get_null_member ("metadata")) {
                var meta = entry.get_object_member ("metadata");
                track_title = get_json_string (meta, "title", track_title);
            }
            var title_label = new Gtk.Label (track_title);
            title_label.add_css_class ("queue-track-title");
            title_label.halign = Gtk.Align.START;
            title_label.ellipsize = Pango.EllipsizeMode.END;
            title_label.max_width_chars = 50;
            text_box.append (title_label);

            var artist = "";
            if (entry.has_member ("metadata") && !entry.get_null_member ("metadata")) {
                var meta = entry.get_object_member ("metadata");
                artist = get_json_string (meta, "artist", "");
            }
            if (artist.length > 0) {
                var artist_label = new Gtk.Label (artist);
                artist_label.add_css_class ("queue-track-artist");
                artist_label.halign = Gtk.Align.START;
                artist_label.ellipsize = Pango.EllipsizeMode.END;
                artist_label.max_width_chars = 50;
                text_box.append (artist_label);
            }

            card.append (text_box);

            /* Duration */
            int64 dur_ms = 0;
            if (entry.has_member ("metadata") && !entry.get_null_member ("metadata")) {
                var meta = entry.get_object_member ("metadata");
                if (meta.has_member ("durationMs")) {
                    dur_ms = meta.get_int_member ("durationMs");
                }
            }
            if (dur_ms > 0) {
                var dur_label = new Gtk.Label (format_duration (dur_ms));
                dur_label.add_css_class ("text-timestamp");
                dur_label.valign = Gtk.Align.CENTER;
                card.append (dur_label);
            }

            row.child = card;
            return row;
        }

        private void on_playlist_back () {
            viewing_playlist_id = null;
            viewing_playlist_name = null;
            playlist_content_box.visible = false;

            if (cached_playlists != null && cached_playlists.length > 0) {
                playlist_list.visible = true;
                playlist_empty_box.visible = false;
            } else {
                playlist_list.visible = false;
                playlist_empty_box.visible = true;
            }
        }

        private void on_load_playlist (string mode) {
            var renderer_id = active_repo.active_renderer_id;
            var server_id = get_selected_playlist_server_id ();

            if (renderer_id.length == 0 || server_id == null ||
                viewing_playlist_id == null) return;

            do_load_playlist.begin (renderer_id, server_id, viewing_playlist_id, mode);
        }

        private async void do_load_playlist (string renderer_id, string server_id,
                                              string playlist_id, string mode) {
            /* 1. Fetch playlist entries */
            var entries = yield playlist_repo.get_playlist (server_id, playlist_id);
            if (entries == null || entries.get_length () == 0) {
                warning ("LibraryView: playlist.get returned empty for %s", playlist_id);
                return;
            }

            /* 2. Parse lib: refs */
            var full_ids = new GenericArray<string> ();
            var lib_ids = new GenericArray<string> ();
            var raw_ids = new GenericArray<string> ();

            for (uint i = 0; i < entries.get_length (); i++) {
                var entry = entries.get_object_element (i);
                var full_id = get_playlist_entry_item_id (entry);
                if (full_id.length == 0) continue;

                string lib_id, raw_id;
                if (split_lib_ref (full_id, out lib_id, out raw_id)) {
                    full_ids.add (full_id);
                    lib_ids.add (lib_id);
                    raw_ids.add (raw_id);
                }
            }

            if (full_ids.length == 0) return;

            /* 3. Resolve each item individually (async, sequential to avoid flood) */
            var queue_entries = new Json.Array ();
            for (uint i = 0; i < full_ids.length; i++) {
                var full_id = full_ids[i];
                var lib_id = lib_ids[i];
                var raw_id = raw_ids[i];

                var resolved = yield library_repo.resolve (lib_id, raw_id, false);
                if (resolved == null) continue;

                string? url = null;
                string? mime = null;
                bool byte_range = false;

                if (resolved.has_member ("sources") && !resolved.get_null_member ("sources")) {
                    var sources = resolved.get_array_member ("sources");
                    if (sources.get_length () > 0) {
                        var src = sources.get_object_element (0);
                        url = src.has_member ("url") ? src.get_string_member ("url") : null;
                        mime = src.has_member ("mime") ? src.get_string_member ("mime") : null;
                        byte_range = src.has_member ("byteRange")
                            ? src.get_boolean_member ("byteRange") : false;
                    }
                }

                Json.Object? metadata = null;
                if (resolved.has_member ("metadata") && !resolved.get_null_member ("metadata")) {
                    metadata = resolved.get_object_member ("metadata");
                }

                if (url != null) {
                    var qe = QueueEntryBuilder.build (full_id, url, mime, byte_range, metadata);
                    queue_entries.add_object_element (qe);
                }
            }

            if (queue_entries.get_length () == 0) {
                warning ("LibraryView: no playable entries resolved");
                return;
            }

            /* 4. Queue the resolved entries */
            var lease = yield lease_mgr.ensure_lease (renderer_id);
            if (lease == null) return;

            if (mode == "replace") {
                var reply = yield correlator.send (
                    renderer_id, "queue.set",
                    QueueBodies.set_entries (0, queue_entries), lease, -1, 10000);
                if (reply != null && reply.ok) {
                    correlator.send_fire_and_forget (
                        renderer_id, "playback.play",
                        PlaybackBodies.play (0), lease);
                }
            } else {
                correlator.send_fire_and_forget (
                    renderer_id, "queue.add",
                    QueueBodies.add ("end", queue_entries), lease);
            }
        }

        private void clear_playlist_content () {
            cached_playlists = null;
            viewing_playlist_id = null;

            /* Clear playlist list */
            Gtk.Widget? child = playlist_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                playlist_list.remove (child);
                child = next;
            }

            /* Clear track list */
            child = playlist_track_list.get_first_child ();
            while (child != null) {
                var next = child.get_next_sibling ();
                playlist_track_list.remove (child);
                child = next;
            }

            playlist_list.visible = false;
            playlist_content_box.visible = false;
            playlist_empty_box.visible = true;
        }

        /* ================================================================
         * Item type detection and metadata extraction
         * ================================================================ */

        private bool is_container_item (Json.Object item) {
            /* Container patterns and explicit overrides — keep in sync with
             * Android LibraryRepository.kt:61 (containerPatterns,
             * explicitContainerTypes, explicitLeafTypes). */
            string item_type = "";
            if (item.has_member ("type") && !item.get_null_member ("type")) {
                item_type = item.get_string_member ("type") ?? "";
            }
            var type_lower = item_type.down ();

            /* Explicit leaves win over patterns. */
            if (type_lower == "podcastepisode") return false;

            /* Explicit containers. */
            if (type_lower == "podcast") return true;

            /* Pattern match: type contains the pattern, OR itemId starts with "{pattern}:". */
            string item_id = "";
            if (item.has_member ("itemId") && !item.get_null_member ("itemId")) {
                item_id = item.get_string_member ("itemId") ?? "";
            } else if (item.has_member ("id") && !item.get_null_member ("id")) {
                item_id = item.get_string_member ("id") ?? "";
            }
            var id_lower = item_id.down ();

            string[] patterns = { "container", "artist", "album", "folder" };
            foreach (var pattern in patterns) {
                if (type_lower.contains (pattern)) return true;
                if (id_lower.has_prefix (pattern + ":")) return true;
            }

            /* Legacy fallbacks the old desktop code relied on. */
            if (item.has_member ("childCount")) return true;
            if (item.has_member ("children")) return true;

            return false;
        }

        private string get_item_id (Json.Object item) {
            if (item.has_member ("itemId")) {
                return item.get_string_member ("itemId");
            }
            if (item.has_member ("id")) {
                return item.get_string_member ("id");
            }
            return "";
        }

        private string get_item_title (Json.Object item) {
            /* Check direct fields */
            if (item.has_member ("title")) {
                var title = item.get_string_member ("title");
                if (title != null && title.length > 0) return title;
            }
            if (item.has_member ("name")) {
                var name = item.get_string_member ("name");
                if (name != null && name.length > 0) return name;
            }

            /* Check nested metadata */
            if (item.has_member ("metadata") && !item.get_null_member ("metadata")) {
                var meta = item.get_object_member ("metadata");
                if (meta.has_member ("title")) {
                    var title = meta.get_string_member ("title");
                    if (title != null && title.length > 0) return title;
                }
            }

            return "Unknown";
        }

        private string get_item_artwork_url (Json.Object item) {
            /* Direct fields: prefer artworkUrl, fall back to imageUrl. */
            if (item.has_member ("artworkUrl") && !item.get_null_member ("artworkUrl")) {
                var url = item.get_string_member ("artworkUrl");
                if (url != null && url.length > 0) return url;
            }
            if (item.has_member ("imageUrl") && !item.get_null_member ("imageUrl")) {
                var url = item.get_string_member ("imageUrl");
                if (url != null && url.length > 0) return url;
            }

            /* Nested metadata. */
            if (item.has_member ("metadata") && !item.get_null_member ("metadata")) {
                var meta = item.get_object_member ("metadata");
                if (meta.has_member ("artworkUrl") && !meta.get_null_member ("artworkUrl")) {
                    var url = meta.get_string_member ("artworkUrl");
                    if (url != null && url.length > 0) return url;
                }
                if (meta.has_member ("imageUrl") && !meta.get_null_member ("imageUrl")) {
                    var url = meta.get_string_member ("imageUrl");
                    if (url != null && url.length > 0) return url;
                }
            }

            return "";
        }

        private string get_item_artist (Json.Object item) {
            /* Try artists[] first (Android-style). */
            var joined = read_artists_array (item);
            if (joined.length > 0) return joined;

            /* Direct artist scalar. */
            if (item.has_member ("artist") && !item.get_null_member ("artist")) {
                var artist = item.get_string_member ("artist");
                if (artist != null && artist.length > 0) return artist;
            }

            /* Nested metadata. */
            if (item.has_member ("metadata") && !item.get_null_member ("metadata")) {
                var meta = item.get_object_member ("metadata");
                var meta_joined = read_artists_array (meta);
                if (meta_joined.length > 0) return meta_joined;
                if (meta.has_member ("artist") && !meta.get_null_member ("artist")) {
                    var artist = meta.get_string_member ("artist");
                    if (artist != null && artist.length > 0) return artist;
                }
            }

            return "";
        }

        private string read_artists_array (Json.Object obj) {
            if (!obj.has_member ("artists") || obj.get_null_member ("artists")) return "";
            var arr = obj.get_array_member ("artists");
            var parts = new GenericArray<string> ();
            for (uint i = 0; i < arr.get_length (); i++) {
                var node = arr.get_element (i);
                if (node.get_node_type () == Json.NodeType.VALUE) {
                    var s = node.get_string ();
                    if (s != null && s.length > 0) parts.add (s);
                }
            }
            if (parts.length == 0) return "";

            var sb = new StringBuilder ();
            for (uint i = 0; i < parts.length; i++) {
                if (i > 0) sb.append (", ");
                sb.append (parts[i]);
            }
            return sb.str;
        }

        private int64 get_item_duration_ms (Json.Object item) {
            if (item.has_member ("durationMs")) {
                return item.get_int_member ("durationMs");
            }

            if (item.has_member ("metadata") && !item.get_null_member ("metadata")) {
                var meta = item.get_object_member ("metadata");
                if (meta.has_member ("durationMs")) {
                    return meta.get_int_member ("durationMs");
                }
            }

            return 0;
        }

        private int64 get_item_child_count (Json.Object item) {
            if (item.has_member ("childCount")) {
                return item.get_int_member ("childCount");
            }
            return 0;
        }

        /* ================================================================
         * Queue entry builders
         * ================================================================ */

        private Json.Object? build_queue_entry_from_resolved (
            string item_id, Json.Object browse_item, Json.Object resolved) {

            /* Extract source URL from resolved object */
            string? url = null;
            string? mime = null;
            bool byte_range = false;

            if (resolved.has_member ("sources") &&
                !resolved.get_null_member ("sources")) {
                var sources = resolved.get_array_member ("sources");
                if (sources.get_length () > 0) {
                    var src = sources.get_object_element (0);
                    url = get_json_string (src, "url", null);
                    mime = get_json_string (src, "mime", null);
                    byte_range = src.has_member ("byteRange")
                        ? src.get_boolean_member ("byteRange") : false;
                }
            }

            /* Fall back to resolved.url */
            if (url == null && resolved.has_member ("url")) {
                url = resolved.get_string_member ("url");
            }

            /* Build metadata from browse item or resolved metadata */
            Json.Object? metadata = null;
            if (resolved.has_member ("metadata") &&
                !resolved.get_null_member ("metadata")) {
                metadata = resolved.get_object_member ("metadata");
            } else if (browse_item.has_member ("metadata") &&
                       !browse_item.get_null_member ("metadata")) {
                metadata = browse_item.get_object_member ("metadata");
            }

            return QueueEntryBuilder.build (item_id, url, mime, byte_range, metadata);
        }

        private Json.Object? build_queue_entry_from_batch_resolved (
            string item_id, Json.Object? browse_item, Json.Object resolved) {

            string? url = null;
            string? mime = null;
            bool byte_range = false;

            if (resolved.has_member ("sources") &&
                !resolved.get_null_member ("sources")) {
                var sources = resolved.get_array_member ("sources");
                if (sources.get_length () > 0) {
                    var src = sources.get_object_element (0);
                    url = get_json_string (src, "url", null);
                    mime = get_json_string (src, "mime", null);
                    byte_range = src.has_member ("byteRange")
                        ? src.get_boolean_member ("byteRange") : false;
                }
            }

            if (url == null && resolved.has_member ("url")) {
                url = resolved.get_string_member ("url");
            }

            Json.Object? metadata = null;
            if (resolved.has_member ("metadata") &&
                !resolved.get_null_member ("metadata")) {
                metadata = resolved.get_object_member ("metadata");
            } else if (browse_item != null &&
                       browse_item.has_member ("metadata") &&
                       !browse_item.get_null_member ("metadata")) {
                metadata = browse_item.get_object_member ("metadata");
            }

            return QueueEntryBuilder.build (item_id, url, mime, byte_range, metadata);
        }

        /* ================================================================
         * Utility helpers
         * ================================================================ */

        private string get_json_string (Json.Object obj, string key, string? fallback) {
            if (obj.has_member (key) && !obj.get_null_member (key)) {
                var val = obj.get_string_member (key);
                if (val != null && val.length > 0) return val;
            }
            return fallback ?? "";
        }

        private string format_duration (int64 total_ms) {
            var total_seconds = (int) (total_ms / 1000);
            var hours = total_seconds / 3600;
            var minutes = (total_seconds % 3600) / 60;
            var seconds = total_seconds % 60;

            if (hours > 0) {
                return "%d:%02d:%02d".printf (hours, minutes, seconds);
            }
            return "%d:%02d".printf (minutes, seconds);
        }
    }
}
