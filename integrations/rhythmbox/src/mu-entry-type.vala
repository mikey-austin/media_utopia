/*
 * Custom RhythmDB entry type for tracks from MU libraries.
 */

namespace Mu {

    public class EntryType : RB.RhythmDBEntryType {

        public EntryType () {
            Object (
                name: "mu-track",
                save_to_disk: false,
                category: RB.RhythmDBEntryCategory.NORMAL
            );
        }
    }
}
