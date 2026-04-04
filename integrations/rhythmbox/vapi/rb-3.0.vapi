/*
 * Hand-crafted Vala bindings for the Rhythmbox API subset used by the MU plugin.
 * Based on /usr/share/gir-1.0/RB-3.0.gir (Rhythmbox 3.x).
 *
 * RhythmDB types don't have the RB prefix in C — they use the RhythmDB prefix.
 * We must set cname explicitly to prevent Vala from prepending "RB".
 */

[CCode (cprefix = "RB", gir_namespace = "RB", gir_version = "3.0", lower_case_cprefix = "rb_")]
namespace RB {

    [CCode (cname = "RhythmDB", cheader_filename = "rhythmdb/rhythmdb.h", type_id = "rhythmdb_get_type ()")]
    public class RhythmDB : GLib.Object {
        [CCode (cname = "rhythmdb_entry_new")]
        public unowned RhythmDBEntry? entry_new (RhythmDBEntryType entry_type, string uri);
        [CCode (cname = "rhythmdb_entry_delete")]
        public void entry_delete (RhythmDBEntry entry);
        [CCode (cname = "rhythmdb_commit")]
        public void commit ();
        [CCode (cname = "rhythmdb_entry_set")]
        public void entry_set (RhythmDBEntry entry, RhythmDBPropType prop, GLib.Value val);
        [CCode (cname = "rhythmdb_entry_get_string")]
        public unowned string entry_get_string (RhythmDBEntry entry, RhythmDBPropType prop);
        [CCode (cname = "rhythmdb_entry_get_ulong")]
        public ulong entry_get_ulong (RhythmDBEntry entry, RhythmDBPropType prop);
        [CCode (cname = "rhythmdb_entry_get_double")]
        public double entry_get_double (RhythmDBEntry entry, RhythmDBPropType prop);
        [CCode (cname = "rhythmdb_register_entry_type")]
        public void register_entry_type (RhythmDBEntryType entry_type);
    }

    [CCode (cname = "RhythmDBEntry", cheader_filename = "rhythmdb/rhythmdb.h", type_id = "rhythmdb_entry_get_type ()", ref_function = "rhythmdb_entry_ref", unref_function = "rhythmdb_entry_unref")]
    [Compact]
    public class RhythmDBEntry {
    }

    [CCode (cname = "RhythmDBEntryType", cheader_filename = "rhythmdb/rhythmdb-entry-type.h", type_id = "rhythmdb_entry_type_get_type ()")]
    public class RhythmDBEntryType : GLib.Object {
        [CCode (has_construct_function = false)]
        protected RhythmDBEntryType ();

        [NoAccessorMethod]
        public string name { owned get; construct; }
        [NoAccessorMethod]
        public bool save_to_disk { get; construct; }
        [NoAccessorMethod]
        public RhythmDBEntryCategory category { get; construct; }
    }

    [CCode (cname = "RhythmDBEntryCategory", cheader_filename = "rhythmdb/rhythmdb-entry-type.h", cprefix = "RHYTHMDB_ENTRY_", has_type_id = false)]
    public enum RhythmDBEntryCategory {
        NORMAL,
        STREAM,
        CONTAINER,
        VIRTUAL
    }

    [CCode (cname = "RhythmDBPropType", cheader_filename = "rhythmdb/rhythmdb.h", cprefix = "RHYTHMDB_PROP_", has_type_id = false)]
    public enum RhythmDBPropType {
        TYPE,
        ENTRY_ID,
        TITLE,
        GENRE,
        ARTIST,
        ALBUM,
        TRACK_NUMBER,
        DISC_NUMBER,
        DURATION,
        FILE_SIZE,
        LOCATION,
        MOUNTPOINT,
        MTIME,
        FIRST_SEEN,
        LAST_SEEN,
        RATING,
        PLAY_COUNT,
        LAST_PLAYED,
        BITRATE,
        DATE,
        TRACK_GAIN,
        TRACK_PEAK,
        ALBUM_GAIN,
        ALBUM_PEAK,
        MEDIA_TYPE,
        TITLE_SORT_KEY,
        GENRE_SORT_KEY,
        ARTIST_SORT_KEY,
        ALBUM_SORT_KEY,
        ALBUM_ARTIST,
        ALBUM_ARTIST_SORT_KEY,
        ARTIST_SORTNAME,
        ALBUM_SORTNAME,
        ALBUM_ARTIST_SORTNAME,
        SUBTITLE,
        DESCRIPTION,
        SUMMARY,
        LANG,
        COPYRIGHT,
        IMAGE,
        POST_TIME,
        MUSICBRAINZ_TRACKID,
        MUSICBRAINZ_ARTISTID,
        MUSICBRAINZ_ALBUMID,
        MUSICBRAINZ_ALBUMARTISTID,
        COMPOSER,
        COMPOSER_SORTNAME,
        COMPOSER_SORT_KEY,
        YEAR,
        BPM,
        COMMENT
    }

    [CCode (cname = "RhythmDBQueryModel", cheader_filename = "rhythmdb/rhythmdb-query-model.h", type_id = "rhythmdb_query_model_get_type ()")]
    public class RhythmDBQueryModel : GLib.Object, Gtk.TreeModel {
        [CCode (cname = "rhythmdb_query_model_new_empty")]
        public RhythmDBQueryModel.empty (RhythmDB db);
        [CCode (cname = "rhythmdb_query_model_add_entry")]
        public void add_entry (RhythmDBEntry entry, int index = -1);
        [CCode (cname = "rhythmdb_query_model_remove_entry")]
        public bool remove_entry (RhythmDBEntry entry);
    }

    [CCode (cheader_filename = "sources/rb-display-page.h", type_id = "rb_display_page_get_type ()")]
    public abstract class DisplayPage : Gtk.Box {
    }

    [CCode (cheader_filename = "sources/rb-display-page-group.h", type_id = "rb_display_page_group_get_type ()")]
    public class DisplayPageGroup : DisplayPage {
        [CCode (cname = "rb_display_page_group_get_by_id")]
        public static unowned DisplayPageGroup get_by_id (string id);
    }

    [CCode (cheader_filename = "sources/rb-source.h", type_id = "rb_source_get_type ()")]
    public abstract class Source : DisplayPage {
        [CCode (has_construct_function = false)]
        protected Source ();

        [NoAccessorMethod]
        public string name { owned get; set; }
    }

    [CCode (cheader_filename = "shell/rb-shell.h", type_id = "rb_shell_get_type ()")]
    public class Shell : GLib.Object {
        [NoAccessorMethod]
        public RhythmDB db { owned get; }
        [NoAccessorMethod]
        public ShellPlayer shell_player { owned get; }
        [NoAccessorMethod]
        public DisplayPageModel display_page_model { owned get; }

        [CCode (cname = "rb_shell_append_display_page")]
        public void append_display_page (DisplayPage page, DisplayPage? parent);
        [CCode (cname = "rb_shell_add_widget")]
        public void add_widget (Gtk.Widget widget, ShellUILocation location, bool expand, bool fill);
        [CCode (cname = "rb_shell_remove_widget")]
        public void remove_widget (Gtk.Widget widget, ShellUILocation location);
    }

    [CCode (cname = "RBShellUILocation", cheader_filename = "shell/rb-shell.h", cprefix = "RB_SHELL_UI_LOCATION_", has_type_id = false)]
    public enum ShellUILocation {
        SIDEBAR,
        RIGHT_SIDEBAR,
        MAIN_TOP,
        MAIN_BOTTOM
    }

    [CCode (cname = "RBPlaylistSource", cheader_filename = "sources/rb-playlist-source.h", type_id = "rb_playlist_source_get_type ()")]
    public abstract class PlaylistSource : Source {
    }

    [CCode (cname = "RBStaticPlaylistSource", cheader_filename = "sources/rb-static-playlist-source.h", type_id = "rb_static_playlist_source_get_type ()")]
    public class StaticPlaylistSource : PlaylistSource {
        [CCode (cname = "rb_static_playlist_source_add_entry")]
        public void add_entry (RhythmDBEntry entry, int index = -1);
        [CCode (cname = "rb_static_playlist_source_remove_entry")]
        public void remove_entry (RhythmDBEntry entry);
    }

    [CCode (cname = "RBPlayer", cheader_filename = "backends/rb-player.h", type_id = "rb_player_get_type ()")]
    public interface Player : GLib.Object {
        public signal void tick (void* stream_data, int64 elapsed, int64 duration);
    }

    [CCode (cheader_filename = "shell/rb-shell-player.h", type_id = "rb_shell_player_get_type ()")]
    public class ShellPlayer : GLib.Object {
        [NoAccessorMethod]
        public GLib.Object player { owned get; }
        [NoAccessorMethod]
        public StaticPlaylistSource queue_source { owned get; set; }
        [CCode (cname = "rb_shell_player_play")]
        public bool play () throws GLib.Error;
        [CCode (cname = "rb_shell_player_pause")]
        public bool pause () throws GLib.Error;
        [CCode (cname = "rb_shell_player_stop")]
        public void stop ();
        [CCode (cname = "rb_shell_player_play_entry")]
        public void play_entry (RhythmDBEntry entry, Source source);
        [CCode (cname = "rb_shell_player_do_next")]
        public bool do_next () throws GLib.Error;
        [CCode (cname = "rb_shell_player_do_previous")]
        public bool do_previous () throws GLib.Error;
        [CCode (cname = "rb_shell_player_get_playing")]
        public bool get_playing (out bool playing) throws GLib.Error;
        [CCode (cname = "rb_shell_player_get_playing_entry")]
        public unowned RhythmDBEntry? get_playing_entry ();
        [CCode (cname = "rb_shell_player_get_playing_time")]
        public bool get_playing_time (out uint time) throws GLib.Error;
        [CCode (cname = "rb_shell_player_set_playing_time")]
        public bool set_playing_time (uint time) throws GLib.Error;
        [CCode (cname = "rb_shell_player_get_playing_song_duration")]
        public long get_playing_song_duration ();
        [CCode (cname = "rb_shell_player_get_volume")]
        public bool get_volume (out double volume) throws GLib.Error;
        [CCode (cname = "rb_shell_player_set_volume")]
        public bool set_volume (double volume) throws GLib.Error;
        [CCode (cname = "rb_shell_player_get_mute")]
        public bool get_mute (out bool mute) throws GLib.Error;
        [CCode (cname = "rb_shell_player_set_mute")]
        public bool set_mute (bool mute) throws GLib.Error;
        [CCode (cname = "rb_shell_player_playpause")]
        public bool playpause () throws GLib.Error;
        [CCode (cname = "rb_shell_player_set_playing_source")]
        public void set_playing_source (Source source);

        public signal void playing_changed (bool playing);
        public signal void playing_song_changed (RhythmDBEntry? entry);
        public signal void elapsed_changed (uint elapsed);
        public signal void playing_source_changed (Source? old_source, Source? new_source);
    }

    [CCode (cheader_filename = "widgets/rb-display-page-model.h", type_id = "rb_display_page_model_get_type ()")]
    public class DisplayPageModel : GLib.Object, Gtk.TreeModel {
    }
}
