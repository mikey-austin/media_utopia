package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// HumanPrinter prints human-readable output.
type HumanPrinter struct{}

// LibraryItemsOutput carries library browse/search payloads with context.
type LibraryItemsOutput struct {
	LibraryID string
	Payload   json.RawMessage
}

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
	case core.PlaylistShowResult:
		return printPlaylistShow(data)
	case core.SnapshotListResult:
		return printSnapshots(data)
	case core.SuggestListResult:
		return printSuggestions(data)
	case core.LibraryResolveResult:
		return printLibraryResolve(data)
	case LibraryItemsOutput:
		return printLibraryItemsOutput(data)
	case core.RawResult:
		return printRaw(data)
	default:
		_, err := fmt.Fprintln(os.Stdout, "ok")
		return err
	}
}

func printNodes(result core.NodesResult) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tKIND\tNODE_ID"); err != nil {
		return err
	}
	for _, node := range result.Nodes {
		_, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", node.Name, node.Kind, node.NodeID)
		if err != nil {
			return err
		}
	}
	return tw.Flush()
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
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	header := "INDEX\tTITLE\tTYPE\tARTIST\tALBUM\tLEN"
	if result.FullIDs {
		header += "\tQUEUE_ID\tITEM_ID"
	}
	if _, err := fmt.Fprintln(tw, header); err != nil {
		return err
	}
	for idx, entry := range result.Queue.Entries {
		title := entry.ItemID
		typ := ""
		artist := ""
		album := ""
		length := ""
		if entry.Metadata != nil {
			if val, ok := entry.Metadata["title"].(string); ok && val != "" {
				title = val
			}
			if val, ok := entry.Metadata["mediaType"].(string); ok && val != "" {
				typ = val
			} else if val, ok := entry.Metadata["type"].(string); ok {
				typ = val
			}
			if val, ok := entry.Metadata["artist"].(string); ok {
				artist = val
			}
			if val, ok := entry.Metadata["album"].(string); ok {
				album = val
			}
			length = formatDuration(entry.Metadata["durationMs"])
		}
		if result.FullIDs {
			_, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", idx, title, typ, artist, album, length, entry.QueueEntryID, entry.ItemID)
			if err != nil {
				return err
			}
			continue
		}
		_, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", idx, title, typ, artist, album, length)
		if err != nil {
			return err
		}
	}
	return tw.Flush()
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
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tPLAYLIST_ID\tREVISION"); err != nil {
		return err
	}
	for _, pl := range result.Playlists {
		_, err := fmt.Fprintf(tw, "%s\t%s\t%d\n", pl.Name, pl.PlaylistID, pl.Revision)
		if err != nil {
			return err
		}
	}
	return tw.Flush()
}

func printPlaylistShow(result core.PlaylistShowResult) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	header := "INDEX\tTITLE\tTYPE\tARTIST\tALBUM\tLEN"
	if result.FullIDs {
		header += "\tENTRY_ID\tITEM_ID"
	}
	if _, err := fmt.Fprintln(tw, header); err != nil {
		return err
	}
	for idx, entry := range result.Entries {
		title := entry.ItemID
		typ := ""
		artist := ""
		album := ""
		length := ""
		if entry.Metadata != nil {
			if val, ok := entry.Metadata["title"].(string); ok && val != "" {
				title = val
			}
			if val, ok := entry.Metadata["mediaType"].(string); ok && val != "" {
				typ = val
			} else if val, ok := entry.Metadata["type"].(string); ok {
				typ = val
			}
			if val, ok := entry.Metadata["artist"].(string); ok {
				artist = val
			}
			if val, ok := entry.Metadata["album"].(string); ok {
				album = val
			}
			length = formatDuration(entry.Metadata["durationMs"])
		}
		if result.FullIDs {
			_, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", idx, title, typ, artist, album, length, entry.EntryID, entry.ItemID)
			if err != nil {
				return err
			}
			continue
		}
		_, err := fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", idx, title, typ, artist, album, length)
		if err != nil {
			return err
		}
	}
	return tw.Flush()
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
	raw, err := rawBytes(result.Data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(raw))
	return err
}

type libraryItemsReply struct {
	Items []libraryItem `json:"items"`
	Start int64         `json:"start"`
	Count int64         `json:"count"`
	Total int64         `json:"total"`
}

type libraryItem struct {
	ItemID      string   `json:"itemId"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	MediaType   string   `json:"mediaType"`
	Artists     []string `json:"artists,omitempty"`
	Album       string   `json:"album,omitempty"`
	ContainerID string   `json:"containerId,omitempty"`
}

func printLibraryItemsOutput(result LibraryItemsOutput) error {
	var payload libraryItemsReply
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "NAME\tTYPE\tARTIST\tALBUM\tCONTAINER_ID\tITEM_ID\tLIB_REF"); err != nil {
		return err
	}
	for _, item := range payload.Items {
		artist := strings.Join(item.Artists, ", ")
		libRef := fmt.Sprintf("lib:%s:%s", result.LibraryID, item.ItemID)
		_, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.Name, item.Type, artist, item.Album, item.ContainerID, item.ItemID, libRef)
		if err != nil {
			return err
		}
	}
	return tw.Flush()
}

func rawBytes(data any) ([]byte, error) {
	switch val := data.(type) {
	case json.RawMessage:
		return val, nil
	case []byte:
		return val, nil
	default:
		out, err := json.Marshal(val)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func formatPosition(pos, dur int64) string {
	if pos == 0 && dur == 0 {
		return ""
	}
	if dur > 0 {
		percent := int64(0)
		if dur > 0 {
			percent = (pos * 100) / dur
		}
		return fmt.Sprintf("%s / %s (%d%%)", formatMS(pos), formatMS(dur), percent)
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

func formatDuration(value any) string {
	switch v := value.(type) {
	case int64:
		return formatMS(v)
	case int:
		return formatMS(int64(v))
	case float64:
		return formatMS(int64(v))
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return formatMS(parsed)
		}
	}
	return ""
}
