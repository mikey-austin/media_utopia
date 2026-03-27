package main

import (
	"testing"
)

func TestParseLibraryTypes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{"empty", "", nil, false},
		{"single", "Audio", []string{"Audio"}, false},
		{"case_insensitive", "audio", []string{"Audio"}, false},
		{"alias", "album", []string{"MusicAlbum"}, false},
		{"multiple", "Audio,MusicAlbum", []string{"Audio", "MusicAlbum"}, false},
		{"with_spaces", " Audio , Video ", []string{"Audio", "Video"}, false},
		{"dedup", "Audio,audio,AUDIO", []string{"Audio"}, false},
		{"artist_alias", "artist", []string{"MusicArtist"}, false},
		{"unknown", "Unknown", nil, true},
		{"mixed_valid_invalid", "Audio,BadType", nil, true},
		{"all_types", "Audio,MusicAlbum,MusicArtist,Movie,Series,Episode,Video,Playlist,Folder",
			[]string{"Audio", "MusicAlbum", "MusicArtist", "Movie", "Series", "Episode", "Video", "Playlist", "Folder"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLibraryTypes(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLibraryTypes(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("parseLibraryTypes(%q) = %v, want %v", tt.input, got, tt.want)
					return
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("parseLibraryTypes(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}
