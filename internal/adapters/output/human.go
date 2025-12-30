package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// HumanPrinter prints human-readable output.
type HumanPrinter struct{}

// Print renders human output.
func (HumanPrinter) Print(v any) error {
	switch data := v.(type) {
	case core.NodesResult:
		return printNodes(data)
	case core.StatusResult:
		return printStatus(data)
	case core.SessionResult:
		return printSession(data)
	case core.QueueResult:
		return printQueue(data)
	case core.QueueNowResult:
		return printQueueNow(data)
	case core.PlaylistListResult:
		return printPlaylists(data)
	case core.SnapshotListResult:
		return printSnapshots(data)
	case core.SuggestListResult:
		return printSuggestions(data)
	case core.LibraryResolveResult:
		return printLibraryResolve(data)
	case core.RawResult:
		return printRaw(data)
	default:
		_, err := fmt.Fprintln(os.Stdout, "ok")
		return err
	}
}

func printNodes(result core.NodesResult) error {
	for _, node := range result.Nodes {
		_, err := fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", node.Name, node.Kind, node.NodeID)
		if err != nil {
			return err
		}
	}
	return nil
}

func printStatus(result core.StatusResult) error {
	status := "unknown"
	position := ""
	volume := ""
	item := ""
	owner := ""
	queue := ""

	if result.State.Playback != nil {
		status = result.State.Playback.Status
		position = formatPosition(result.State.Playback.PositionMS, result.State.Playback.DurationMS)
		volume = fmt.Sprintf("vol %d%%", int(result.State.Playback.Volume*100+0.5))
		if result.State.Playback.Mute {
			volume = "muted"
		}
	}
	if result.State.Current != nil {
		item = formatItem(result.State.Current)
	}
	if result.State.Queue != nil {
		queue = fmt.Sprintf("Queue: %d tracks (index %d) rev %d", result.State.Queue.Length, result.State.Queue.Index, result.State.Queue.Revision)
	}
	if result.State.Session != nil {
		owner = fmt.Sprintf("owner %s", result.State.Session.Owner)
	}

	line := strings.TrimSpace(fmt.Sprintf("%s  [%s]  %s  %s  %s", result.Renderer.Name, status, item, position, volume))
	if _, err := fmt.Fprintln(os.Stdout, line); err != nil {
		return err
	}
	if queue != "" || owner != "" {
		_, err := fmt.Fprintf(os.Stdout, "%s %s\n", queue, owner)
		return err
	}
	return nil
}

func printSession(result core.SessionResult) error {
	expires := time.Unix(result.Session.LeaseExpiresAt, 0).Format(time.RFC3339)
	_, err := fmt.Fprintf(os.Stdout, "session %s expires %s\n", result.Session.ID, expires)
	return err
}

func printQueue(result core.QueueResult) error {
	for idx, entry := range result.Queue.Entries {
		label := entry.ItemID
		if title, ok := entry.Metadata["title"].(string); ok {
			label = title
		}
		_, err := fmt.Fprintf(os.Stdout, "%d\t%s\t%s\n", idx, entry.QueueEntryID, label)
		if err != nil {
			return err
		}
	}
	return nil
}

func printQueueNow(result core.QueueNowResult) error {
	if result.Current == nil {
		_, err := fmt.Fprintln(os.Stdout, "(none)")
		return err
	}
	item := formatItem(result.Current)
	_, err := fmt.Fprintln(os.Stdout, item)
	return err
}

func printPlaylists(result core.PlaylistListResult) error {
	for _, pl := range result.Playlists {
		_, err := fmt.Fprintf(os.Stdout, "%s\t%s\t%d\n", pl.Name, pl.PlaylistID, pl.Revision)
		if err != nil {
			return err
		}
	}
	return nil
}

func printSnapshots(result core.SnapshotListResult) error {
	for _, snap := range result.Snapshots {
		_, err := fmt.Fprintf(os.Stdout, "%s\t%s\t%d\n", snap.Name, snap.SnapshotID, snap.Revision)
		if err != nil {
			return err
		}
	}
	return nil
}

func printSuggestions(result core.SuggestListResult) error {
	for _, sug := range result.Suggestions {
		_, err := fmt.Fprintf(os.Stdout, "%s\t%s\t%d\n", sug.Name, sug.SuggestionID, sug.Revision)
		if err != nil {
			return err
		}
	}
	return nil
}

func printLibraryResolve(result core.LibraryResolveResult) error {
	if len(result.Item.Sources) == 0 {
		_, err := fmt.Fprintln(os.Stdout, "no sources")
		return err
	}
	for _, src := range result.Item.Sources {
		_, err := fmt.Fprintf(os.Stdout, "%s\t%s\n", src.URL, src.Mime)
		if err != nil {
			return err
		}
	}
	return nil
}

func printRaw(result core.RawResult) error {
	data, ok := result.Data.(json.RawMessage)
	if !ok {
		_, err := fmt.Fprintln(os.Stdout, result.Data)
		return err
	}
	_, err := fmt.Fprintln(os.Stdout, string(data))
	return err
}

func formatPosition(pos, dur int64) string {
	if pos == 0 && dur == 0 {
		return ""
	}
	return fmt.Sprintf("%s / %s", formatMS(pos), formatMS(dur))
}

func formatMS(ms int64) string {
	if ms <= 0 {
		return "0:00"
	}
	secs := ms / 1000
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

func formatItem(current *mu.CurrentItemState) string {
	if current.Metadata != nil {
		title, _ := current.Metadata["title"].(string)
		artist, _ := current.Metadata["artist"].(string)
		if title != "" && artist != "" {
			return fmt.Sprintf("%s - %s", artist, title)
		}
		if title != "" {
			return title
		}
	}
	return current.ItemID
}
