/*
 * RB.Source subclass for browsing an MU library.
 * One instance per discovered library node.
 *
 * Supports browsing containers, searching, and resolving tracks to
 * HTTP stream URLs for playback through Rhythmbox.
 */

namespace Mu {

    public class LibrarySource : RB.Source {

        private RB.Shell shell;
        private MqttClient mqtt;
        private Config config;
        private Presence library_presence;
        private Mu.EntryType entry_type;
        private weak Controller? controller;

        /* Browse state. */
        private GenericArray<string> container_stack;
        private string current_container_id = "";

        /* Display model. */
        private Gtk.ListStore list_store;
        private Gtk.TreeView tree_view;
        private Gtk.ScrolledWindow scroll;
        private Gtk.SearchEntry search_entry;
        private Gtk.Box content_box;
        private Gtk.Button back_button;

        private const int COL_ICON = 0;
        private const int COL_NAME = 1;
        private const int COL_ARTIST = 2;
        private const int COL_ALBUM = 3;
        private const int COL_ITEM_ID = 4;
        private const int COL_IS_CONTAINER = 5;
        private const int N_COLS = 6;

        public LibrarySource (RB.Shell shell, MqttClient mqtt, Config config,
                              Presence library_presence, Mu.EntryType entry_type,
                              Controller? controller) {
            Object (name: library_presence.name);
            this.shell = shell;
            this.mqtt = mqtt;
            this.config = config;
            this.library_presence = library_presence;
            this.entry_type = entry_type;
            this.controller = controller;
            this.container_stack = new GenericArray<string> ();

            build_ui ();
            browse_container ("");
        }

        public string get_library_node_id () {
            return library_presence.node_id;
        }

        private void build_ui () {
            content_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);

            var toolbar = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 6);
            toolbar.margin = 6;

            back_button = new Gtk.Button.from_icon_name (
                "go-previous-symbolic", Gtk.IconSize.BUTTON);
            back_button.sensitive = false;
            back_button.clicked.connect (on_back);
            toolbar.pack_start (back_button, false, false, 0);

            search_entry = new Gtk.SearchEntry ();
            search_entry.placeholder_text = "Search library...";
            search_entry.activate.connect (on_search);
            toolbar.pack_start (search_entry, true, true, 0);

            content_box.pack_start (toolbar, false, false, 0);

            list_store = new Gtk.ListStore (N_COLS,
                typeof (string), typeof (string), typeof (string),
                typeof (string), typeof (string), typeof (bool));

            tree_view = new Gtk.TreeView.with_model (list_store);
            tree_view.headers_visible = true;

            var icon_renderer = new Gtk.CellRendererPixbuf ();
            var icon_col = new Gtk.TreeViewColumn.with_attributes (
                "", icon_renderer, "icon-name", COL_ICON);
            icon_col.fixed_width = 30;
            tree_view.append_column (icon_col);

            var name_col = new Gtk.TreeViewColumn.with_attributes (
                "Name", new Gtk.CellRendererText (), "text", COL_NAME);
            name_col.expand = true;
            tree_view.append_column (name_col);

            var artist_col = new Gtk.TreeViewColumn.with_attributes (
                "Artist", new Gtk.CellRendererText (), "text", COL_ARTIST);
            artist_col.fixed_width = 200;
            tree_view.append_column (artist_col);

            var album_col = new Gtk.TreeViewColumn.with_attributes (
                "Album", new Gtk.CellRendererText (), "text", COL_ALBUM);
            album_col.fixed_width = 200;
            tree_view.append_column (album_col);

            tree_view.row_activated.connect (on_row_activated);
            tree_view.button_press_event.connect (on_button_press);

            /* Enable multi-select. */
            tree_view.get_selection ().set_mode (Gtk.SelectionMode.MULTIPLE);

            scroll = new Gtk.ScrolledWindow (null, null);
            scroll.add (tree_view);
            content_box.pack_start (scroll, true, true, 0);

            this.pack_start (content_box, true, true, 0);
            this.show_all ();
        }

        /* -- Browse ------------------------------------------------- */

        private void browse_container (string container_id) {
            current_container_id = container_id;
            back_button.sensitive = container_stack.length > 0;

            string target = topic_commands (config.topic_base,
                library_presence.node_id);
            string reply_topic = topic_reply (config.topic_base,
                config.build_controller_id ());
            var body = build_library_browse_body (container_id, 0, 200);

            mqtt.build_and_send (target, "library.browse", body,
                config.build_controller_id (), reply_topic, null,
                (reply) => {
                    if (!reply.ok || reply.body == null) {
                        string err = "unknown";
                        if (reply.err != null) err = @"$(reply.err.code): $(reply.err.message)";
                        message ("MU library browse failed (%s): %s",
                            library_presence.name, err);
                        return;
                    }
                    populate_from_reply (reply.body);
                });
        }

        private void populate_from_reply (Json.Node body) {
            list_store.clear ();
            var obj = body.get_object ();
            if (obj == null || !obj.has_member ("items")) return;

            var items = obj.get_array_member ("items");
            items.foreach_element ((arr, i, node) => {
                var item = node.get_object ();
                if (item == null) return;

                string item_name = "";
                string artist = "";
                string album = "";
                string item_id = "";
                bool is_container = false;

                if (item.has_member ("name"))
                    item_name = item.get_string_member ("name");
                if (item.has_member ("itemId"))
                    item_id = item.get_string_member ("itemId");

                /* Determine if this is a container or a playable track.
                 * The MU browse response uses "type" — "Folder" means container,
                 * "Audio"/"Video" means playable. */
                if (item.has_member ("type")) {
                    string itype = item.get_string_member ("type");
                    is_container = (itype == "Folder" || itype == "container" ||
                                    itype == "MusicArtist" || itype == "MusicAlbum");
                }

                /* Extract artist — can be a string or "artists" array. */
                if (item.has_member ("artists")) {
                    var artists_node = item.get_member ("artists");
                    if (artists_node.get_node_type () == Json.NodeType.ARRAY) {
                        var arr2 = artists_node.get_array ();
                        if (arr2.get_length () > 0)
                            artist = arr2.get_string_element (0);
                    }
                }
                if (artist.length == 0 && item.has_member ("artist"))
                    artist = item.get_string_member ("artist");

                if (item.has_member ("album"))
                    album = item.get_string_member ("album");

                /* Fallback: check nested metadata object. */
                if (item.has_member ("metadata")) {
                    var meta = item.get_object_member ("metadata");
                    if (artist.length == 0 && meta.has_member ("artist"))
                        artist = meta.get_string_member ("artist");
                    if (album.length == 0 && meta.has_member ("album"))
                        album = meta.get_string_member ("album");
                    if (item_name.length == 0 && meta.has_member ("title"))
                        item_name = meta.get_string_member ("title");
                }

                Gtk.TreeIter iter;
                list_store.append (out iter);
                list_store.set (iter,
                    COL_ICON, is_container ? "folder" : "audio-x-generic",
                    COL_NAME, item_name,
                    COL_ARTIST, artist,
                    COL_ALBUM, album,
                    COL_ITEM_ID, item_id,
                    COL_IS_CONTAINER, is_container);
            });
        }

        /* -- Search ------------------------------------------------- */

        private void on_search () {
            string query = search_entry.text.strip ();
            if (query.length == 0) {
                browse_container (current_container_id);
                return;
            }

            string target = topic_commands (config.topic_base,
                library_presence.node_id);
            string reply_topic = topic_reply (config.topic_base,
                config.build_controller_id ());
            var body = build_library_search_body (query, 0, 200);

            mqtt.build_and_send (target, "library.search", body,
                config.build_controller_id (), reply_topic, null,
                (reply) => {
                    if (!reply.ok || reply.body == null) return;
                    populate_from_reply (reply.body);
                });
        }

        /* -- Navigation --------------------------------------------- */

        private void on_row_activated (Gtk.TreePath path,
                                       Gtk.TreeViewColumn col) {
            Gtk.TreeIter iter;
            if (!list_store.get_iter (out iter, path)) return;

            Value is_container_val;
            Value item_id_val;
            list_store.get_value (iter, COL_IS_CONTAINER, out is_container_val);
            list_store.get_value (iter, COL_ITEM_ID, out item_id_val);

            bool is_container = is_container_val.get_boolean ();
            string item_id = item_id_val.get_string ();

            if (is_container) {
                container_stack.add (current_container_id);
                browse_container (item_id);
            } else {
                resolve_and_play (item_id);
            }
        }

        private void on_back () {
            if (container_stack.length == 0) return;
            string parent = container_stack[container_stack.length - 1];
            container_stack.remove_index (container_stack.length - 1);
            browse_container (parent);
        }

        /* -- Context menu ------------------------------------------- */

        private bool on_button_press (Gdk.EventButton event) {
            if (event.button != 3) return false;  /* Right-click only. */

            var menu = new Gtk.Menu ();

            var play_item = new Gtk.MenuItem.with_label ("Play");
            play_item.activate.connect (() => { action_on_selected (true); });
            menu.append (play_item);

            var enqueue_item = new Gtk.MenuItem.with_label ("Add to Queue");
            enqueue_item.activate.connect (() => { action_on_selected (false); });
            menu.append (enqueue_item);

            menu.show_all ();
            menu.popup_at_pointer (event);
            return false;
        }

        /**
         * Resolve and enqueue/play all selected track rows.
         */
        private void action_on_selected (bool play_first) {
            var selection = tree_view.get_selection ();
            var rows = selection.get_selected_rows (null);
            bool first = true;
            rows.foreach ((path) => {
                Gtk.TreeIter iter;
                if (!list_store.get_iter (out iter, path)) return;

                Value is_container_val, item_id_val;
                list_store.get_value (iter, COL_IS_CONTAINER, out is_container_val);
                list_store.get_value (iter, COL_ITEM_ID, out item_id_val);

                if (is_container_val.get_boolean ()) return;  /* Skip containers. */

                string item_id = item_id_val.get_string ();
                bool should_play = play_first && first;
                first = false;
                resolve_and_enqueue (item_id, should_play);
            });
        }

        /* -- Resolve and play --------------------------------------- */

        private void resolve_and_play (string item_id) {
            resolve_and_enqueue (item_id, true);
        }

        private void resolve_and_enqueue (string item_id, bool play) {
            if (controller == null || !controller.can_command ()) {
                message ("MU library: no renderer selected or no lease — can't play/enqueue");
                return;
            }

            string target = topic_commands (config.topic_base,
                library_presence.node_id);
            string reply_topic = topic_reply (config.topic_base,
                config.build_controller_id ());
            var body = build_library_resolve_body (item_id, false);

            mqtt.build_and_send (target, "library.resolve", body,
                config.build_controller_id (), reply_topic, null,
                (reply) => {
                    if (!reply.ok || reply.body == null) {
                        message ("MU library: resolve failed for %s", item_id);
                        return;
                    }
                    var robj = reply.body.get_object ();
                    if (robj == null || !robj.has_member ("sources")) return;

                    var sources = robj.get_array_member ("sources");
                    if (sources.get_length () == 0) return;

                    var first = sources.get_object_element (0);
                    if (first == null || !first.has_member ("url")) return;

                    string url = first.get_string_member ("url");
                    string mime = "";
                    if (first.has_member ("mime"))
                        mime = first.get_string_member ("mime");

                    string full_item_id = @"lib:$(library_presence.node_id):$(item_id)";

                    if (play) {
                        controller.queue_add_and_play (full_item_id, url, mime);
                    } else {
                        controller.queue_add (full_item_id, url, mime);
                    }
                });
        }
    }
}
